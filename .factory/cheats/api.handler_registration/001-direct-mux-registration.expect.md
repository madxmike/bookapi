# 001 direct mux registration

The route table is the only allowed registration point. `buildMux` is the one
function allowed to call `Handle`, and only in the `range s.routes()` loop.
Adding `mux.HandleFunc` inside `buildMux` must be rejected by
`TestRouteTableIsTheOnlyRegistration`.

Expected violation: `route registration is not table-driven`.
