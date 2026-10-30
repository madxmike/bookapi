# 002 orphan handler

A handler-shaped method that is not referenced from `s.routes()` must be
rejected by `TestRouteTableIsTheOnlyRegistration`.

Expected violation: `handler httpapi|server|orphan is not reachable through the
route table`.
