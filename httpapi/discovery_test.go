package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"os"
	"os/exec"
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
	muxField      = "mux"
)

// checkedPackage is one type-checked package from the module under test.
type checkedPackage struct {
	path  string
	name  string
	files []*ast.File
	pkg   *types.Package
	info  *types.Info
}

// module is the loaded and type-checked module.
type module struct {
	fset *token.FileSet
	imp  types.Importer
	pkgs []checkedPackage
}

// TestRouteTableIsTheOnlyRegistration is the discovery check for
// api.handler_registration. It type-checks the module, so aliases and
// assignability are resolved rather than matched by spelling.
func TestRouteTableIsTheOnlyRegistration(t *testing.T) {
	m := loadModule(t, "..")
	if violations := checkModule(m); len(violations) > 0 {
		t.Fatalf("route registration violations:\n%s", strings.Join(violations, "\n"))
	}
}

// loadModule runs go list for export data, parses each main-module package,
// and type-checks it from source. Export data is used only for imports, so
// unexported declarations in the package under test remain visible.
func loadModule(t *testing.T, root string) *module {
	t.Helper()

	cmd := exec.Command("go", "list", "-export", "-deps", "-json", "./...")
	cmd.Dir = root
	env := make([]string, 0, len(os.Environ())+1)
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "GOWORK=") {
			env = append(env, e)
		}
	}
	cmd.Env = append(env, "GOWORK=off")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", root, err, stderr.String())
	}

	type listedPackage struct {
		ImportPath string
		Name       string
		Dir        string
		GoFiles    []string
		Export     string
		Module     *struct {
			Main bool
		}
	}

	dec := json.NewDecoder(bytes.NewReader(out))
	exports := map[string]string{}
	var mainPkgs []listedPackage
	for {
		var p listedPackage
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("decode go list: %v", err)
		}
		if p.Export != "" {
			exports[p.ImportPath] = p.Export
		}
		if p.Module != nil && p.Module.Main {
			mainPkgs = append(mainPkgs, p)
		}
	}

	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %q", path)
		}
		return os.Open(file)
	})

	m := &module{fset: fset, imp: imp}
	for _, p := range mainPkgs {
		var files []*ast.File
		for _, name := range p.GoFiles {
			af, err := parser.ParseFile(fset, filepath.Join(p.Dir, name), nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", filepath.Join(p.Dir, name), err)
			}
			files = append(files, af)
		}
		if len(files) == 0 {
			continue
		}

		info := &types.Info{
			Types:      map[ast.Expr]types.TypeAndValue{},
			Defs:       map[*ast.Ident]types.Object{},
			Uses:       map[*ast.Ident]types.Object{},
			Implicits:  map[ast.Node]types.Object{},
			Selections: map[*ast.SelectorExpr]*types.Selection{},
			Scopes:     map[ast.Node]*types.Scope{},
		}
		var typeErrs []string
		conf := types.Config{
			Importer: imp,
			Error:    func(err error) { typeErrs = append(typeErrs, err.Error()) },
		}
		tpkg, err := conf.Check(p.ImportPath, fset, files, info)
		if err != nil || len(typeErrs) > 0 {
			t.Fatalf("type-check %s: %v\n%s", p.ImportPath, err, strings.Join(typeErrs, "\n"))
		}
		m.pkgs = append(m.pkgs, checkedPackage{
			path:  p.ImportPath,
			name:  p.Name,
			files: files,
			pkg:   tpkg,
			info:  info,
		})
	}
	return m
}

// checkModule returns every route-registration or handler-reachability
// violation in m. It has no spelling-based fallbacks: aliases of the writer
// and of *response resolve through go/types.
func checkModule(m *module) []string {
	apiPkg := packageNamed(m, tablePackage)
	if apiPkg == nil {
		return []string{fmt.Sprintf("package %s not found", tablePackage)}
	}

	httpPkg, err := m.imp.Import("net/http")
	if err != nil {
		return []string{fmt.Sprintf("import net/http: %v", err)}
	}
	rwObj := httpPkg.Scope().Lookup("ResponseWriter")
	reqObj := httpPkg.Scope().Lookup("Request")
	if rwObj == nil || reqObj == nil {
		return []string{"net/http is missing ResponseWriter or Request"}
	}
	requestType := types.NewPointer(reqObj.Type())

	responseObj := apiPkg.pkg.Scope().Lookup("response")
	if responseObj == nil {
		return []string{"httpapi.response not found"}
	}
	serverObj := apiPkg.pkg.Scope().Lookup(tableReceiver)
	if serverObj == nil {
		return []string{"httpapi.server not found"}
	}

	httpTarget := newSignature(rwObj.Type(), requestType)
	responseTarget := newSignature(types.NewPointer(responseObj.Type()), requestType)
	shape := func(sig *types.Signature) bool {
		bare := withoutReceiver(sig)
		return types.AssignableTo(bare, httpTarget) || types.AssignableTo(bare, responseTarget)
	}

	var violations []string

	// The route table is the only source of reachability.
	reachable := map[*types.Func]bool{}
	tableFD := findMethod(apiPkg, tableFunc, tableReceiver)
	if tableFD == nil {
		violations = append(violations, fmt.Sprintf("%s.(*%s).%s not found", apiPkg.path, tableReceiver, tableFunc))
	} else {
		serverPtr := types.NewPointer(serverObj.Type())
		ast.Inspect(tableFD.Body, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok || !isRouteLiteral(apiPkg, cl) {
				return true
			}
			expr := routeHandlerExpr(cl)
			if expr == nil {
				violations = append(violations, fmt.Sprintf("%s: route entry has no handler", position(m, cl)))
				return true
			}
			fn := funcObject(apiPkg.info, expr)
			if fn == nil {
				violations = append(violations, fmt.Sprintf("%s: route handler must be a named function", position(m, expr)))
				return true
			}
			sig := fn.Type().(*types.Signature)
			if !shape(sig) {
				violations = append(violations, fmt.Sprintf("%s: route handler %s is not handler-shaped", position(m, expr), fn.FullName()))
				return true
			}
			if fn.Pkg() != apiPkg.pkg {
				violations = append(violations, fmt.Sprintf("%s: route handler %s is outside package %s", position(m, expr), fn.FullName(), apiPkg.path))
				return true
			}
			if recv := sig.Recv(); recv != nil && !types.Identical(recv.Type(), serverPtr) {
				violations = append(violations, fmt.Sprintf("%s: route handler %s must be a method of *%s", position(m, expr), fn.FullName(), tableReceiver))
				return true
			}
			reachable[fn] = true
			return true
		})
	}

	// The boundary is the one handler-shaped function that is not a route: it
	// is the table's dispatcher. It must also be structurally single-path.
	var boundaryObj *types.Func
	var allowedDispatch *ast.CallExpr
	boundaryFD := findMethod(apiPkg, boundaryFunc, tableReceiver)
	if boundaryFD == nil {
		violations = append(violations, fmt.Sprintf("%s.(*%s).%s not found", apiPkg.path, tableReceiver, boundaryFunc))
	} else {
		boundaryObj, _ = apiPkg.info.Defs[boundaryFD.Name].(*types.Func)
		if boundaryObj == nil || !shape(boundaryObj.Type().(*types.Signature)) {
			violations = append(violations, fmt.Sprintf("%s: %s is not handler-shaped", position(m, boundaryFD), boundaryFunc))
		}
		var vs []string
		allowedDispatch, vs = checkServeHTTP(m, apiPkg, boundaryFD)
		violations = append(violations, vs...)
	}

	// Registration has exactly one allowed shape: the table loop in buildMux.
	var allowedHandle *ast.CallExpr
	attachFD := findMethod(apiPkg, attachFunc, tableReceiver)
	if attachFD == nil {
		violations = append(violations, fmt.Sprintf("%s.(*%s).%s not found", apiPkg.path, tableReceiver, attachFunc))
	} else {
		var vs []string
		allowedHandle, vs = checkBuildMux(m, apiPkg, attachFD)
		violations = append(violations, vs...)
	}

	// Every handler-shaped function must be reachable from the table.
	for i := range m.pkgs {
		p := &m.pkgs[i]
		for _, f := range p.files {
			for _, decl := range f.Decls {
				fd, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				obj, ok := p.info.Defs[fd.Name].(*types.Func)
				if !ok {
					continue
				}
				sig := obj.Type().(*types.Signature)
				if !shape(sig) {
					continue
				}
				if boundaryObj != nil && obj == boundaryObj {
					continue
				}
				if !reachable[obj] {
					violations = append(violations, fmt.Sprintf("%s: handler %s is not reachable through the route table", position(m, fd), obj.FullName()))
				}
			}
		}
	}

	// Handler-shaped function literals are handlers too. The only allowed one
	// is adapt's closure, which returns the http.HandlerFunc the mux calls.
	for i := range m.pkgs {
		p := &m.pkgs[i]
		for _, f := range p.files {
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if p.name == tablePackage && d.Recv == nil && d.Name.Name == "adapt" {
						continue
					}
					if d.Body == nil {
						continue
					}
					violations = append(violations, checkAnonymousHandlers(m, p, d.Body, shape)...)
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						vs, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for _, value := range vs.Values {
							violations = append(violations, checkAnonymousHandlers(m, p, value, shape)...)
						}
					}
				}
			}
		}
	}

	// References and method values that bypass the table or the boundary.
	violations = append(violations, checkReferences(m, allowedHandle, allowedDispatch)...)

	return violations
}

func checkServeHTTP(m *module, p *checkedPackage, fd *ast.FuncDecl) (*ast.CallExpr, []string) {
	var violations []string
	recvName := receiverIdent(fd)

	var topDispatches []*ast.CallExpr
	for _, stmt := range fd.Body.List {
		switch s := stmt.(type) {
		case *ast.IfStmt, *ast.ForStmt, *ast.RangeStmt, *ast.SwitchStmt, *ast.TypeSwitchStmt, *ast.SelectStmt, *ast.BranchStmt, *ast.GoStmt, *ast.ReturnStmt:
			violations = append(violations, fmt.Sprintf("%s: ServeHTTP must be structurally single-path", position(m, s)))
		case *ast.ExprStmt:
			call, ok := s.X.(*ast.CallExpr)
			if !ok {
				violations = append(violations, fmt.Sprintf("%s: unexpected top-level expression in ServeHTTP", position(m, s)))
				continue
			}
			fn := funcObject(p.info, call.Fun)
			if fn == nil || fn.Name() != boundaryFunc {
				violations = append(violations, fmt.Sprintf("%s: unexpected top-level call in ServeHTTP", position(m, call)))
				continue
			}
			topDispatches = append(topDispatches, call)
		}
	}

	var calls []*ast.CallExpr
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch n.(type) {
		case *ast.GoStmt:
			violations = append(violations, fmt.Sprintf("%s: ServeHTTP must not start goroutines", position(m, n)))
		case *ast.BranchStmt:
			violations = append(violations, fmt.Sprintf("%s: ServeHTTP must not branch", position(m, n)))
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if fn := funcObject(p.info, call.Fun); fn != nil && fn.Name() == boundaryFunc {
			calls = append(calls, call)
		}
		return true
	})
	if len(calls) != 1 || len(topDispatches) != 1 || calls[0] != topDispatches[0] {
		violations = append(violations, fmt.Sprintf("%s: ServeHTTP must have exactly one unconditional dispatch to the mux", position(m, fd)))
		return nil, violations
	}

	dispatch := topDispatches[0]
	sel, ok := dispatch.Fun.(*ast.SelectorExpr)
	if !ok {
		violations = append(violations, fmt.Sprintf("%s: ServeHTTP dispatch must call the mux", position(m, dispatch)))
		return nil, violations
	}
	recv, ok := sel.X.(*ast.SelectorExpr)
	if !ok || !isIdent(recv.X, recvName) || recv.Sel.Name != muxField {
		violations = append(violations, fmt.Sprintf("%s: ServeHTTP dispatch must be on %s.%s", position(m, dispatch), recvName, muxField))
		return nil, violations
	}
	if !isServeMuxMethod(funcObject(p.info, dispatch.Fun), boundaryFunc) {
		violations = append(violations, fmt.Sprintf("%s: ServeHTTP dispatch must use http.ServeMux.ServeHTTP", position(m, dispatch)))
		return nil, violations
	}
	if recvType, ok := p.info.Types[recv].Type.(*types.Pointer); ok {
		if !isNamed(recvType.Elem(), "net/http", "ServeMux") {
			violations = append(violations, fmt.Sprintf("%s: ServeHTTP dispatch receiver is not *http.ServeMux", position(m, dispatch)))
			return nil, violations
		}
	} else {
		violations = append(violations, fmt.Sprintf("%s: ServeHTTP dispatch receiver is not *http.ServeMux", position(m, dispatch)))
		return nil, violations
	}
	return dispatch, violations
}

func checkBuildMux(m *module, p *checkedPackage, fd *ast.FuncDecl) (*ast.CallExpr, []string) {
	bad := func() (*ast.CallExpr, []string) {
		return nil, []string{fmt.Sprintf("%s: buildMux must be the single table-driven registration loop", position(m, fd))}
	}
	if len(fd.Body.List) != 3 {
		return bad()
	}

	assign, ok := fd.Body.List[0].(*ast.AssignStmt)
	if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
		return bad()
	}
	muxIdent, ok := assign.Lhs[0].(*ast.Ident)
	if !ok {
		return bad()
	}
	ctor, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok || !isNetHTTPFunc(funcObject(p.info, ctor.Fun), "NewServeMux") {
		return bad()
	}

	rng, ok := fd.Body.List[1].(*ast.RangeStmt)
	if !ok || len(rng.Body.List) != 1 {
		return bad()
	}
	rtIdent, ok := rng.Value.(*ast.Ident)
	if !ok {
		return bad()
	}
	routesCall, ok := rng.X.(*ast.CallExpr)
	if !ok {
		return bad()
	}
	routesSel, ok := routesCall.Fun.(*ast.SelectorExpr)
	if !ok || routesSel.Sel.Name != tableFunc || !isIdent(routesSel.X, receiverIdent(fd)) {
		return bad()
	}

	stmt, ok := rng.Body.List[0].(*ast.ExprStmt)
	if !ok {
		return bad()
	}
	handle, ok := stmt.X.(*ast.CallExpr)
	if !ok || len(handle.Args) != 2 {
		return bad()
	}
	handleSel, ok := handle.Fun.(*ast.SelectorExpr)
	if !ok || handleSel.Sel.Name != "Handle" || !isIdent(handleSel.X, muxIdent.Name) {
		return bad()
	}
	if !isServeMuxMethod(funcObject(p.info, handle.Fun), "Handle") {
		return bad()
	}
	patternCall, ok := handle.Args[0].(*ast.CallExpr)
	if !ok {
		return bad()
	}
	patternSel, ok := patternCall.Fun.(*ast.SelectorExpr)
	if !ok || patternSel.Sel.Name != "path" || !isIdent(patternSel.X, rtIdent.Name) {
		return bad()
	}
	adaptCall, ok := handle.Args[1].(*ast.CallExpr)
	if !ok || len(adaptCall.Args) != 1 {
		return bad()
	}
	adaptIdent, ok := adaptCall.Fun.(*ast.Ident)
	if !ok || adaptIdent.Name != "adapt" {
		return bad()
	}
	handlerSel, ok := adaptCall.Args[0].(*ast.SelectorExpr)
	if !ok || handlerSel.Sel.Name != "handler" || !isIdent(handlerSel.X, rtIdent.Name) {
		return bad()
	}

	ret, ok := fd.Body.List[2].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 || !isIdent(ret.Results[0], muxIdent.Name) {
		return bad()
	}
	return handle, nil
}

func checkAnonymousHandlers(m *module, p *checkedPackage, root ast.Node, shape func(*types.Signature) bool) []string {
	var violations []string
	ast.Inspect(root, func(n ast.Node) bool {
		lit, ok := n.(*ast.FuncLit)
		if !ok {
			return true
		}
		tv, ok := p.info.Types[lit]
		if !ok {
			return true
		}
		sig, ok := tv.Type.(*types.Signature)
		if !ok || !shape(sig) {
			return true
		}
		violations = append(violations, fmt.Sprintf("%s: anonymous handler is not reachable through the route table", position(m, lit)))
		return true
	})
	return violations
}

func checkReferences(m *module, allowedHandle, allowedDispatch *ast.CallExpr) []string {
	var violations []string
	for i := range m.pkgs {
		p := &m.pkgs[i]
		for _, f := range p.files {
			for _, decl := range f.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Body != nil {
						violations = append(violations, scanReferences(m, p, d.Body, allowedHandle, allowedDispatch)...)
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						vs, ok := spec.(*ast.ValueSpec)
						if !ok {
							continue
						}
						for _, value := range vs.Values {
							violations = append(violations, scanReferences(m, p, value, allowedHandle, allowedDispatch)...)
						}
					}
				}
			}
		}
	}
	return violations
}

func scanReferences(m *module, p *checkedPackage, root ast.Node, allowedHandle, allowedDispatch *ast.CallExpr) []string {
	callFuns := map[ast.Expr]bool{}
	ast.Inspect(root, func(n ast.Node) bool {
		if call, ok := n.(*ast.CallExpr); ok {
			callFuns[call.Fun] = true
		}
		return true
	})

	var violations []string
	ast.Inspect(root, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.Ident:
			obj := p.info.Uses[e]
			if isDefaultServeMux(obj) {
				violations = append(violations, fmt.Sprintf("%s: reference to http.DefaultServeMux", position(m, e)))
			}
			if isReflectMakeFunc(obj) {
				violations = append(violations, fmt.Sprintf("%s: reference to reflect.MakeFunc", position(m, e)))
			}
		case *ast.SelectorExpr:
			obj := p.info.Uses[e.Sel]
			if isDefaultServeMux(obj) {
				violations = append(violations, fmt.Sprintf("%s: reference to http.DefaultServeMux", position(m, e)))
			}
			if isReflectMakeFunc(obj) {
				violations = append(violations, fmt.Sprintf("%s: reference to reflect.MakeFunc", position(m, e)))
			}
			if isNetHTTPPackageFunc(obj, "Handle") || isNetHTTPPackageFunc(obj, "HandleFunc") {
				violations = append(violations, fmt.Sprintf("%s: reference to http.Handle/http.HandleFunc", position(m, e)))
			}
			if sel, ok := p.info.Selections[e]; ok && !callFuns[e] {
				kind := sel.Kind()
				if kind == types.MethodVal || kind == types.MethodExpr {
					if fn, ok := sel.Obj().(*types.Func); ok {
						switch fn.Name() {
						case "Handle", "HandleFunc", "ServeHTTP":
							violations = append(violations, fmt.Sprintf("%s: method value %s bypasses the route table", position(m, e), fn.FullName()))
						}
					}
					if isServeMuxType(sel.Recv()) {
						violations = append(violations, fmt.Sprintf("%s: method value on http.ServeMux bypasses the route table", position(m, e)))
					}
				}
			}
		case *ast.CallExpr:
			fn := funcObject(p.info, e.Fun)
			if fn == nil {
				return true
			}
			switch fn.Name() {
			case "Handle", "HandleFunc":
				if e != allowedHandle {
					violations = append(violations, fmt.Sprintf("%s: route registration outside the route table", position(m, e)))
				}
			case "ServeHTTP":
				if e != allowedDispatch {
					violations = append(violations, fmt.Sprintf("%s: ServeHTTP dispatch outside the single boundary", position(m, e)))
				}
			}
		}
		return true
	})
	return violations
}

func newSignature(params ...types.Type) *types.Signature {
	vars := make([]*types.Var, len(params))
	for i, p := range params {
		vars[i] = types.NewParam(token.NoPos, nil, "", p)
	}
	return types.NewSignatureType(nil, nil, nil, types.NewTuple(vars...), types.NewTuple(), false)
}

func withoutReceiver(sig *types.Signature) *types.Signature {
	if sig.Recv() == nil {
		return sig
	}
	return types.NewSignatureType(nil, nil, nil, sig.Params(), sig.Results(), sig.Variadic())
}

func packageNamed(m *module, name string) *checkedPackage {
	for i := range m.pkgs {
		if m.pkgs[i].name == name {
			return &m.pkgs[i]
		}
	}
	return nil
}

func findMethod(p *checkedPackage, name, recv string) *ast.FuncDecl {
	for _, f := range p.files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if ok && fd.Name.Name == name && receiverTypeName(p.info, fd) == recv {
				return fd
			}
		}
	}
	return nil
}

func receiverTypeName(info *types.Info, fd *ast.FuncDecl) string {
	obj, ok := info.Defs[fd.Name].(*types.Func)
	if !ok {
		return ""
	}
	recv := obj.Type().(*types.Signature).Recv()
	if recv == nil {
		return ""
	}
	return namedTypeName(recv.Type())
}

func receiverIdent(fd *ast.FuncDecl) string {
	if fd.Recv == nil || len(fd.Recv.List) != 1 || len(fd.Recv.List[0].Names) != 1 {
		return ""
	}
	return fd.Recv.List[0].Names[0].Name
}

func namedTypeName(t types.Type) string {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	if named, ok := t.(*types.Named); ok {
		return named.Obj().Name()
	}
	return ""
}

func isNamed(t types.Type, pkgPath, name string) bool {
	named, ok := t.(*types.Named)
	return ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == pkgPath && named.Obj().Name() == name
}

func isServeMuxType(t types.Type) bool {
	ptr, ok := t.(*types.Pointer)
	if !ok {
		return false
	}
	return isNamed(ptr.Elem(), "net/http", "ServeMux")
}

func isServeMuxMethod(obj *types.Func, name string) bool {
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != "net/http" || obj.Name() != name {
		return false
	}
	sig := obj.Type().(*types.Signature)
	return sig.Recv() != nil && isServeMuxType(sig.Recv().Type())
}

func isNetHTTPFunc(obj *types.Func, name string) bool {
	return obj != nil && obj.Pkg() != nil && obj.Pkg().Path() == "net/http" && obj.Name() == name && obj.Type().(*types.Signature).Recv() == nil
}

func isNetHTTPPackageFunc(obj types.Object, name string) bool {
	fn, ok := obj.(*types.Func)
	return ok && isNetHTTPFunc(fn, name)
}

func isDefaultServeMux(obj types.Object) bool {
	v, ok := obj.(*types.Var)
	return ok && v.Pkg() != nil && v.Pkg().Path() == "net/http" && v.Name() == "DefaultServeMux"
}

func isReflectMakeFunc(obj types.Object) bool {
	fn, ok := obj.(*types.Func)
	return ok && fn.Pkg() != nil && fn.Pkg().Path() == "reflect" && fn.Name() == "MakeFunc"
}

func funcObject(info *types.Info, expr ast.Expr) *types.Func {
	switch e := expr.(type) {
	case *ast.Ident:
		fn, _ := info.Uses[e].(*types.Func)
		return fn
	case *ast.SelectorExpr:
		if sel, ok := info.Selections[e]; ok {
			fn, _ := sel.Obj().(*types.Func)
			return fn
		}
		fn, _ := info.Uses[e.Sel].(*types.Func)
		return fn
	}
	return nil
}

func isRouteLiteral(p *checkedPackage, cl *ast.CompositeLit) bool {
	tv, ok := p.info.Types[cl]
	if !ok {
		return false
	}
	return isNamed(tv.Type, p.path, "route")
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
	if len(cl.Elts) >= 3 {
		if _, keyed := cl.Elts[0].(*ast.KeyValueExpr); !keyed {
			return cl.Elts[2]
		}
	}
	return nil
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

func position(m *module, node ast.Node) string {
	return m.fset.Position(node.Pos()).String()
}

const routeFixture = `package httpapi

import "net/http"

type response struct{}

type handler func(*response, *http.Request)

type route struct {
	method  string
	pattern string
	handler handler
}

func (rt route) path() string { return rt.method + " " + rt.pattern }

type server struct {
	mux *http.ServeMux
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

func adapt(h handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { h(&response{}, r) }
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}
`

func checkFixture(t *testing.T, src string) []string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	pkgDir := filepath.Join(dir, "httpapi")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "fixture.go"), []byte(src), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	violations := checkModule(loadModule(t, dir))
	t.Logf("fixture violations:\n%s", strings.Join(violations, "\n"))
	return violations
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

func TestRouteFixtureIsClean(t *testing.T) {
	if violations := checkFixture(t, routeFixture); len(violations) > 0 {
		t.Fatalf("fixture violations:\n%s", strings.Join(violations, "\n"))
	}
}

// (a) A method value of DefaultServeMux.HandleFunc plus a handler declared
// with an alias of http.ResponseWriter.
func TestDiscoveryRejectsDefaultMuxMethodValueAndWriterAlias(t *testing.T) {
	src := routeFixture + `
type W = http.ResponseWriter

func handlerWithAlias(w W, r *http.Request) {}

func init() {
	f := http.DefaultServeMux.HandleFunc
	f("/x", handlerWithAlias)
}
`
	violations := checkFixture(t, src)
	requireViolation(t, violations, "reference to http.DefaultServeMux")
	requireViolation(t, violations, "method value")
	requireViolation(t, violations, "handler fixture/httpapi.handlerWithAlias is not reachable through the route table")
}

// (b) A renamed helper inside buildMux that registers a route outside the
// table.
func TestDiscoveryRejectsRenamedRegistrationHelper(t *testing.T) {
	src := strings.Replace(routeFixture, `	for _, rt := range s.routes() {
		mux.Handle(rt.path(), adapt(rt.handler))
	}
	return mux`, `	for _, rt := range s.routes() {
		mux.Handle(rt.path(), adapt(rt.handler))
	}
	extra := func(h http.HandlerFunc) {
		mux.HandleFunc("/extra", h)
	}
	extra(adapt(s.get))
	return mux`, 1)
	if src == routeFixture {
		t.Fatal("fixture replacement did not apply")
	}

	violations := checkFixture(t, src)
	requireViolation(t, violations, "buildMux must be the single table-driven registration loop")
	requireViolation(t, violations, "route registration outside the route table")
}

// (c) A second dispatch path in ServeHTTP through DefaultServeMux.
func TestDiscoveryRejectsSecondDispatchPath(t *testing.T) {
	src := strings.Replace(routeFixture, `	s.mux.ServeHTTP(w, r)
}`, `	s.mux.ServeHTTP(w, r)
	http.DefaultServeMux.ServeHTTP(w, r)
}`, 1)
	if src == routeFixture {
		t.Fatal("fixture replacement did not apply")
	}

	violations := checkFixture(t, src)
	requireViolation(t, violations, "reference to http.DefaultServeMux")
	requireViolation(t, violations, "exactly one unconditional dispatch")
}

// (d) A handler manufactured by reflect.MakeFunc.
func TestDiscoveryRejectsReflectMakeFunc(t *testing.T) {
	src := strings.Replace(routeFixture, `import "net/http"`, `import (
	"net/http"
	"reflect"
)`, 1)
	src += `
var backdoor = reflect.MakeFunc(reflect.TypeOf(func() {}), nil)
`
	violations := checkFixture(t, src)
	requireViolation(t, violations, "reference to reflect.MakeFunc")
}

// (e) A handler reachable only through the catch-all, declared with an alias
// of *response.
func TestDiscoveryRejectsCatchAllAliasHandler(t *testing.T) {
	src := strings.Replace(routeFixture, `		route{method: http.MethodGet, pattern: "/books/{isbn}", handler: s.get},`, `		route{method: http.MethodGet, pattern: "/books/{isbn}", handler: s.get},
		route{method: "", pattern: "/", handler: s.notFound},`, 1)
	src += `
type R = response

func (s *server) notFound(w *response, r *http.Request) {
	if r.Method == http.MethodGet {
		s.backdoor(w, r)
	}
}

func (s *server) backdoor(w *R, r *http.Request) {}
`
	violations := checkFixture(t, src)
	requireViolation(t, violations, "backdoor is not reachable through the route table")
}

func TestDiscoveryRejectsDirectMuxRegistration(t *testing.T) {
	src := strings.Replace(routeFixture, `	}
	return mux
}`, `	}
	mux.HandleFunc("/backdoor", func(w http.ResponseWriter, r *http.Request) {})
	return mux
}`, 1)
	violations := checkFixture(t, src)
	requireViolation(t, violations, "route registration outside the route table")
	requireViolation(t, violations, "buildMux must be the single table-driven registration loop")
}

func TestDiscoveryRejectsRegistrationOutsideBuildMux(t *testing.T) {
	src := routeFixture + `
func setup(mux *http.ServeMux) { mux.HandleFunc("/backdoor", nil) }
`
	violations := checkFixture(t, src)
	requireViolation(t, violations, "route registration outside the route table")
}

func TestDiscoveryRejectsLiteralPathInTheTableLoop(t *testing.T) {
	src := strings.Replace(routeFixture, `mux.Handle(rt.path(), adapt(rt.handler))`, `mux.Handle("/backdoor", adapt(rt.handler))`, 1)
	violations := checkFixture(t, src)
	requireViolation(t, violations, "buildMux must be the single table-driven registration loop")
}

func TestDiscoveryRejectsHandlerOutsideRouteTable(t *testing.T) {
	src := routeFixture + `
func orphan(w http.ResponseWriter, r *http.Request) {}
`
	violations := checkFixture(t, src)
	requireViolation(t, violations, "handler fixture/httpapi.orphan is not reachable through the route table")
}

func TestDiscoveryRejectsAnonymousRouteHandler(t *testing.T) {
	src := strings.Replace(routeFixture, `handler: s.get`, `handler: func(w *response, r *http.Request) {}`, 1)
	violations := checkFixture(t, src)
	requireViolation(t, violations, "route handler must be a named function")
}

func TestDiscoveryRejectsHandlerStoredInAVariable(t *testing.T) {
	src := routeFixture + `
var backdoor = func(w http.ResponseWriter, r *http.Request) {}
`
	violations := checkFixture(t, src)
	requireViolation(t, violations, "anonymous handler is not reachable through the route table")
}
