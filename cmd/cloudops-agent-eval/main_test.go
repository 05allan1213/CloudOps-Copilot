package main

import (
	"path/filepath"
	"testing"
)

func TestEvalPathsSelectsFrozenRevision(t *testing.T) {
	paths, err := evalPaths("/repo", "v2")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/repo", "eval", "v2", "dataset.json"); paths.dataset != want {
		t.Fatalf("dataset path=%q, want %q", paths.dataset, want)
	}
	if want := filepath.Join("/repo", "eval", "v2", "thresholds.json"); paths.thresholds != want {
		t.Fatalf("threshold path=%q, want %q", paths.thresholds, want)
	}
}

func TestEvalPathsRejectsTraversalAndAmbiguousRevisions(t *testing.T) {
	for _, revision := range []string{"", "v0", "v01", "../v2", "v2/../v1", "latest"} {
		if _, err := evalPaths("/repo", revision); err == nil {
			t.Fatalf("revision %q unexpectedly accepted", revision)
		}
	}
}
