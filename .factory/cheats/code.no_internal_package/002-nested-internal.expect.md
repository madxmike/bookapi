# nested internal directory

Property: code.no_internal_package
Bypass: add internal under an existing package instead of at the module root.
Must be rejected by: TestNoInternalPackage
Expected verdict: reject
Notes: the rule is any depth, not only the top level.
