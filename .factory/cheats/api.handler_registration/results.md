# `api.handler_registration` negative controls

Both patches apply cleanly to the working tree, are rejected by the type-aware
`TestRouteTableIsTheOnlyRegistration`, and pass again after `git apply -R`.
Full output: `.factory/evidence/cheat-api.handler_registration.txt`.

| Patch | Reintroduction | Observed rejection |
|---|---|---|
| `001-direct-mux-registration.patch` | `mux.HandleFunc("GET /backdoor", ...)` inside `buildMux` | `buildMux must be the single table-driven registration loop`, `route registration outside the route table`, `anonymous handler is not reachable through the route table` |
| `002-orphan-handler.patch` | `func (s *server) orphan(w *response, r *http.Request) {}` not in `routes` | `handler (*example.com/bookapi/httpapi.server).orphan is not reachable through the route table` |

The same checker is exercised by `httpapi/discovery_test.go` against executable
fixtures for:

- `http.DefaultServeMux.HandleFunc` taken as a method value, with a
  `type W = http.ResponseWriter` handler;
- a renamed helper inside `buildMux`;
- a second `http.DefaultServeMux.ServeHTTP` dispatch;
- `reflect.MakeFunc`;
- a handler reachable only through the catch-all with `type R = response`;
- direct registration outside `buildMux`;
- a literal path in the table loop;
- an orphan handler-shaped function;
- an anonymous handler in a table entry;
- a handler stored in a package-level variable.
