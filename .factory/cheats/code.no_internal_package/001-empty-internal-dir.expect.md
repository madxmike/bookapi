# empty internal directory

Property: code.no_internal_package
Bypass: restore the internal directory with a single placeholder file.
Must be rejected by: TestNoInternalPackage
Expected verdict: reject
Notes: the directory is what Go treats specially, even if nothing imports it.
