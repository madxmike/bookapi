package main

import (
	"io/fs"
	"path/filepath"
	"testing"
)

// TestNoInternalPackage enforces D12: the module has no directory named
// internal at any depth, and no Go package path containing /internal/.
func TestNoInternalPackage(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return fs.SkipDir
		}
		if d.Name() == "internal" && (d.IsDir() || d.Type()&fs.ModeSymlink != 0) {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			t.Errorf("forbidden internal package: %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
