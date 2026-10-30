# `api.handler_registration` negative controls

Both patches apply cleanly to the working tree, are rejected by
`TestRouteTableIsTheOnlyRegistration`, and pass again after
`git apply -R`. Full output: `.factory/evidence/cheat-api.handler_registration.txt`.

| Patch | Reintroduction | Observed rejection |
|---|---|---|
| `001-direct-mux-registration.patch` | `mux.HandleFunc("GET /backdoor", ...)` inside `buildMux` | `route registration is not table-driven` and `anonymous handler is not reachable through the route table` |
| `002-orphan-handler.patch` | `func (s *server) orphan(w *response, r *http.Request) {}` not in `routes` | `handler httpapi|server|orphan is not reachable through the route table` |

The same checker is also exercised by `httpapi/discovery_test.go` against
synthetic sources for a handler registered outside `buildMux`, a literal path
in the table loop, a handler-shaped function absent from the table, and an
anonymous handler in a table entry.
