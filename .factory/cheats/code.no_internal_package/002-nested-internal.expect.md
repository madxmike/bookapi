# 002: restore a nested `httpapi/internal/`

**Reintroduction.** A placeholder file under an existing package, so the rule
must apply at any depth and not only at the module root.

**Must be rejected by.** `TestNoInternalPackage`, which names the violating
path.

**Expected verdict.** Rejected.
