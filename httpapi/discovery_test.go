package httpapi

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	tablePackage  = "httpapi"
	tableFunc     = "routes"
	tableReceiver = "server"
	attachFunc    = "buildMux"
	boundaryFunc  = "ServeHTTP"
)

type sourceFile struct {
	name string
	src  []byte
}

// TestRouteTableIsTheOnlyRegistration is the discovery check for
// api.handler_registration. It parses the module and fails if:
//   - a handler-shaped function is not referenced by the route table;
//   - a route is registered anywhere except the table-driven loop in buildMux;
//   - the table-driven loop uses anything other than adapt(rt.handler).
//
// Test files are excluded: they contain fakes and helpers, not routes.
func TestRouteTableIsTheOnlyRegistration(t *testing.T) {
	var files []sourceFile
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "vendor" || d.Name() == ".factory" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, sourceFile{name: path, src: src})
		return nil
	})
	if err != nil {
		t.Fatalf("walk module: %v", err)
	}

	if violations := checkRouteTable(files); len(violations) > 0 {
		t.Fatalf("route registration violations:\n%s", strings.Join(violations, "\n"))
	}
}

func checkRouteTable(files []sourceFile) []string {
	fset := token.NewFileSet()
	type parsed struct {
		pkg  string
		file *ast.File
	}

	var (
		pkgs        []parsed
		violations  []string
		reachable   = map[string]bool{}
		foundTable  bool
		foundAttach bool
		tableRecv   string
	)

	for _, f := range files {
		af, err := parser.ParseFile(fset, f.name, f.src, 0)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s: parse: %v", f.name, err))
			continue
		}
		pkgs = append(pkgs, parsed{pkg: af.Name.Name, file: af})
	}

	// The route table: collect the handler named in every entry.
	for _, pf := range pkgs {
		if pf.pkg != tablePackage {
			continue
		}
		for _, decl := range pf.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != tableFunc {
				continue
			}
			recv, ok := receiverType(fd)
			if !ok || recv != tableReceiver {
				continue
			}
			foundTable = true
			tableRecv = receiverName(fd)
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				cl, ok := n.(*ast.CompositeLit)
				if !ok || !isIdent(cl.Type, "route") {
					return true
				}
				expr := routeHandlerExpr(cl)
				if expr == nil {
					violations = append(violations, fmt.Sprintf("%s: route entry has no handler", fset.Position(cl.Pos())))
					return true
				}
				switch h := expr.(type) {
				case *ast.Ident:
					reachable[tablePackage+"||"+h.Name] = true
				case *ast.SelectorExpr:
					x, ok := h.X.(*ast.Ident)
					if !ok || x.Name != tableRecv {
						violations = append(violations, fmt.Sprintf("%s: route handler must be a function of %s, not %s", fset.Position(h.Pos()), tableRecv, exprString(h)))
						return true
					}
					reachable[tablePackage+"|"+tableReceiver+"|"+h.Sel.Name] = true
				default:
					violations = append(violations, fmt.Sprintf("%s: route handler must be a named function", fset.Position(expr.Pos())))
				}
				return true
			})
		}
	}
	if !foundTable {
		violations = append(violations, fmt.Sprintf("%s.%s not found", tablePackage, tableFunc))
	}

	// Every handler-shaped function must be reachable, except the one
	// ServeHTTP dispatcher that carries the table into the mux.
	boundaryFound := false
	boundaryDispatches := false
	for _, pf := range pkgs {
		for _, decl := range pf.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || !isHandlerShape(fd.Type) {
				continue
			}
			recv, _ := receiverType(fd)
			key := pf.pkg + "||" + fd.Name.Name
			if recv != "" {
				key = pf.pkg + "|" + recv + "|" + fd.Name.Name
			}
			if pf.pkg == tablePackage && recv == tableReceiver && fd.Name.Name == boundaryFunc {
				boundaryFound = true
				if callsServeHTTP(fd.Body) {
					boundaryDispatches = true
				}
				continue
			}
			if !reachable[key] {
				violations = append(violations, fmt.Sprintf("%s: handler %s is not reachable through the route table", fset.Position(fd.Pos()), key))
			}
		}
	}
	if !boundaryFound {
		violations = append(violations, fmt.Sprintf("%s.(*%s).%s not found", tablePackage, tableReceiver, boundaryFunc))
	} else if !boundaryDispatches {
		violations = append(violations, fmt.Sprintf("%s.(*%s).%s does not dispatch through the mux", tablePackage, tableReceiver, boundaryFunc))
	}

	// A handler-shaped function literal is still a handler. The only allowed
	// one is adapt's closure, which returns the real http.HandlerFunc the mux
	// calls. Everything else is unreachable by name and therefore by the table.
	for _, pf := range pkgs {
		exempt := map[token.Pos]bool{}
		for _, decl := range pf.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			recv, _ := receiverType(fd)
			if pf.pkg != tablePackage || recv != "" || fd.Name.Name != "adapt" {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if lit, ok := n.(*ast.FuncLit); ok {
					exempt[lit.Pos()] = true
				}
				return true
			})
		}
		ast.Inspect(pf.file, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if !ok || exempt[lit.Pos()] || !isHandlerShape(lit.Type) {
				return true
			}
			violations = append(violations, fmt.Sprintf("%s: anonymous handler is not reachable through the route table", fset.Position(lit.Pos())))
			return true
		})
	}

	// Route registration: Handle/HandleFunc may only appear in buildMux, and
	// there only in the loop over the route table.
	for _, pf := range pkgs {
		for _, decl := range pf.file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			recv, _ := receiverType(fd)
			isAttach := pf.pkg == tablePackage && recv == tableReceiver && fd.Name.Name == attachFunc
			if isAttach {
				foundAttach = true
			}
			loopVar := ""
			if isAttach {
				loopVar = routeLoopVar(fd)
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if sel.Sel.Name != "Handle" && sel.Sel.Name != "HandleFunc" {
					return true
				}
				if !isAttach {
					violations = append(violations, fmt.Sprintf("%s: direct route registration outside the route table", fset.Position(call.Pos())))
					return true
				}
				if sel.Sel.Name == "HandleFunc" || !tableDriven(call, loopVar) {
					violations = append(violations, fmt.Sprintf("%s: route registration is not table-driven", fset.Position(call.Pos())))
				}
				return true
			})
		}
	}
	if !foundAttach {
		violations = append(violations, fmt.Sprintf("%s.(*%s).%s not found", tablePackage, tableReceiver, attachFunc))
	}

	return violations
}

func routeHandlerExpr(cl *ast.CompositeLit) ast.Expr {
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "handler" {
			return kv.Value
		}
	}
	// Unkeyed entries put the handler third: method, pattern, handler.
	if len(cl.Elts) >= 3 {
		if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); !keyed {
			return cl.Elts[2]
		}
	}
	return nil
}

func routeLoopVar(fd *ast.FuncDecl) string {
	var name string
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		rng, ok := n.(*ast.RangeStmt)
		if !ok {
			return true
		}
		call, ok := rng.X.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != tableFunc {
			return true
		}
		if id, ok := rng.Value.(*ast.Ident); ok {
			name = id.Name
		}
		return false
	})
	return name
}

func tableDriven(call *ast.CallExpr, loopVar string) bool {
	if loopVar == "" || len(call.Args) != 2 {
		return false
	}
	pattern, ok := call.Args[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	patternFun, ok := pattern.Fun.(*ast.SelectorExpr)
	if !ok || patternFun.Sel.Name != "path" {
		return false
	}
	if x, ok := patternFun.X.(*ast.Ident); !ok || x.Name != loopVar {
		return false
	}
	wrapped, ok := call.Args[1].(*ast.CallExpr)
	if !ok {
		return false
	}
	wrapFun, ok := wrapped.Fun.(*ast.Ident)
	if !ok || wrapFun.Name != "adapt" || len(wrapped.Args) != 1 {
		return false
	}
	handler, ok := wrapped.Args[0].(*ast.SelectorExpr)
	if !ok {
		return false
	}
	if x, ok := handler.X.(*ast.Ident); !ok || x.Name != loopVar {
		return false
	}
	return handler.Sel.Name == "handler"
}

func callsServeHTTP(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == boundaryFunc {
			found = true
		}
		return true
	})
	return found
}

func isHandlerShape(ft *ast.FuncType) bool {
	if ft.Results != nil && len(ft.Results.List) > 0 {
		return false
	}
	params := paramTypes(ft.Params)
	if len(params) != 2 {
		return false
	}
	return isResponseWriter(params[0]) && isRequestPtr(params[1])
}

func paramTypes(fl *ast.FieldList) []ast.Expr {
	if fl == nil {
		return nil
	}
	var out []ast.Expr
	for _, field := range fl.List {
		n := len(field.Names)
		if n == 0 {
			n = 1
		}
		for i := 0; i < n; i++ {
			out = append(out, field.Type)
		}
	}
	return out
}

func isResponseWriter(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "ResponseWriter"
	case *ast.StarExpr:
		return isIdent(t.X, "response") || isIdent(t.X, "Response")
	case *ast.Ident:
		return t.Name == "ResponseWriter"
	}
	return false
}

func isRequestPtr(e ast.Expr) bool {
	star, ok := e.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch t := star.X.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name == "Request"
	case *ast.Ident:
		return t.Name == "Request"
	}
	return false
}

func receiverType(fd *ast.FuncDecl) (string, bool) {
	if fd.Recv == nil || len(fd.Recv.List) != 1 {
		return "", false
	}
	return baseTypeName(fd.Recv.List[0].Type)
}

func baseTypeName(e ast.Expr) (string, bool) {
	switch t := e.(type) {
	case *ast.StarExpr:
		return baseTypeName(t.X)
	case *ast.Ident:
		return t.Name, true
	}
	return "", false
}

func receiverName(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
		return ""
	}
	return fd.Recv.List[0].Names[0].Name
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func exprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.SelectorExpr:
		return exprString(t.X) + "." + t.Sel.Name
	case *ast.StarExpr:
		return "*" + exprString(t.X)
	}
	return fmt.Sprintf("%T", e)
}

const routeFixture = `package httpapi

import "net/http"

type response struct{}

type route struct {
	method  string
	pattern string
	handler func(*response, *http.Request)
}

func (rt route) path() string { return rt.method + " " + rt.pattern }

type server struct {
	mux http.Handler
}

func (s *server) get(w *response, r *http.Request) {}

func (s *server) routes() []route {
	return []route{
		route{method: http.MethodGet, pattern: "/books/{isbn}", handler: s.get},
	}
}

func (s *server) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	for _, rt := range s.routes() {
		mux.Handle(rt.path(), adapt(rt.handler))
	}
	return mux
}

func adapt(h func(*response, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { h(&response{}, r) }
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
`

func checkFixture(t *testing.T, src string) []string {
	t.Helper()
	return checkRouteTable([]sourceFile{{name: "fixture.go", src: []byte(src)}})
}

func requireViolation(t *testing.T, violations []string, want string) {
	t.Helper()
	for _, v := range violations {
		if strings.Contains(v, want) {
			return
		}
	}
	t.Fatalf("violations = %v, want one containing %q", violations, want)
}

func TestDiscoveryRejectsDirectMuxRegistration(t *testing.T) {
	src := strings.Replace(routeFixture, "\treturn mux\n", "\tmux.HandleFunc(\"/backdoor\", extra)\n\treturn mux\n", 1)
	src += "\nfunc extra(w http.ResponseWriter, r *http.Request) {}\n"

	violations := checkFixture(t, src)
	requireViolation(t, violations, "route registration is not table-driven")
	requireViolation(t, violations, "handler httpapi||extra is not reachable through the route table")
}

func TestDiscoveryRejectsRegistrationOutsideBuildMux(t *testing.T) {
	src := routeFixture + "\nfunc setup(mux *http.ServeMux) { mux.Handle(\"/backdoor\", nil) }\n"

	violations := checkFixture(t, src)
	requireViolation(t, violations, "direct route registration outside the route table")
}

func TestDiscoveryRejectsLiteralPathInTheTableLoop(t *testing.T) {
	src := strings.Replace(routeFixture, "mux.Handle(rt.path(), adapt(rt.handler))", "mux.Handle(\"/backdoor\", adapt(rt.handler))", 1)

	violations := checkFixture(t, src)
	requireViolation(t, violations, "route registration is not table-driven")
}

func TestDiscoveryRejectsHandlerOutsideRouteTable(t *testing.T) {
	src := routeFixture + "\nfunc orphan(w http.ResponseWriter, r *http.Request) {}\n"

	violations := checkFixture(t, src)
	requireViolation(t, violations, "handler httpapi||orphan is not reachable through the route table")
}

func TestDiscoveryRejectsAnonymousRouteHandler(t *testing.T) {
	src := strings.Replace(routeFixture, "handler: s.get", "handler: func(w *response, r *http.Request) {}", 1)

	violations := checkFixture(t, src)
	requireViolation(t, violations, "route handler must be a named function")
}

func TestDiscoveryRejectsHandlerStoredInAVariable(t *testing.T) {
	src := routeFixture + "\nvar backdoor = func(w http.ResponseWriter, r *http.Request) {}\n"

	violations := checkFixture(t, src)
	requireViolation(t, violations, "anonymous handler is not reachable through the route table")
}

func TestRouteFixtureIsClean(t *testing.T) {
	if violations := checkFixture(t, routeFixture); len(violations) > 0 {
		t.Fatalf("fixture violations: %v", violations)
	}
}
