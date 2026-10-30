# `api.head_book` negative control

`001-head-503-204.patch` makes the read handler answer 204 for HEAD when
storage is unavailable, while GET still answers 503. It applies cleanly, is
rejected, and passes again after `git apply -R`. Full output:
`.factory/evidence/cheat-api.head_book.txt`.

Observed failure on a real `httptest.NewServer`:

```
--- FAIL: TestHeadMirrorsEveryGetStatus/503
    list_head_test.go:224: HEAD status = 204, want 503 (body "")
```

The parity test also covers 200, 400, 404, and 500.
