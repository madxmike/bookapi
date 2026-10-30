# `api.handler_uniform` negative control

`001-missing-nosniff.patch` removes the `X-Content-Type-Options` assignment
from `response.flush`. It applies cleanly, is rejected, and passes again after
`git apply -R`. Full output:
`.factory/evidence/cheat-api.handler_uniform.txt`.

Observed failure on every route in `TestEveryRouteIsUniform` and every case in
`TestEveryErrorClassIsUniform`:

```
X-Content-Type-Options = "", want nosniff
```

The uniformity test walks `s.routes()`, so the failure covers health,
readiness, the book routes, HEAD, DELETE, and the catch-all 404 without a
per-route test.
