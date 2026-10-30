# 001 missing nosniff

The common headers are applied once in `response.flush`. Removing
`X-Content-Type-Options` must be rejected by `TestEveryRouteIsUniform` and
`TestEveryErrorClassIsUniform`.

Expected failure: `X-Content-Type-Options = ""` on every route.
