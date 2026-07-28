package main

import (
	"os"
	"path/filepath"
	"testing"
)

const testDatasetAddress = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestEvalPathsSelectsContentAddressedDataset(t *testing.T) {
	paths, err := evalPaths("/repo", testDatasetAddress)
	if err != nil {
		t.Fatal(err)
	}
	if paths.datasetAddress != testDatasetAddress {
		t.Fatalf("dataset address=%q, want %q", paths.datasetAddress, testDatasetAddress)
	}
	if want := filepath.Join("/repo", "eval", "sha256-"+testDatasetAddress, "dataset.json"); paths.dataset != want {
		t.Fatalf("dataset path=%q, want %q", paths.dataset, want)
	}
	if want := filepath.Join("/repo", "eval", "sha256-"+testDatasetAddress, "thresholds.json"); paths.thresholds != want {
		t.Fatalf("threshold path=%q, want %q", paths.thresholds, want)
	}
	if len(paths.runtimeSources) != 7 || paths.runtimeSources[0] != filepath.Join("/repo", "internal", "taskhandler", "investigation_start.go") {
		t.Fatalf("runtime source bindings=%v", paths.runtimeSources)
	}
}

func TestEvalPathsUsesActiveIndex(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "eval"), 0o700); err != nil {
		t.Fatal(err)
	}
	index := []byte(`{"schema_version":1,"active_dataset_sha256":"` + testDatasetAddress + `"}`)
	if err := os.WriteFile(filepath.Join(root, "eval", "index.json"), index, 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := evalPaths(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if paths.datasetAddress != testDatasetAddress {
		t.Fatalf("dataset address=%q, want %q", paths.datasetAddress, testDatasetAddress)
	}
}

func TestEvalPathsRejectsTraversalAndAmbiguousAddresses(t *testing.T) {
	for _, address := range []string{"0", "sha256:" + testDatasetAddress, "../" + testDatasetAddress, "ABCDEF0123456789abcdef0123456789abcdef0123456789abcdef0123456789"} {
		if _, err := evalPaths("/repo", address); err == nil {
			t.Fatalf("dataset address %q unexpectedly accepted", address)
		}
	}
}

func TestVerifyDatasetAddress(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dataset.json")
	contents := []byte("content-addressed dataset\n")
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyDatasetAddress(path, "e9423ce0a707cd0d139b31055021e60fa38da1b2ec3ff603392e3b275f45dcae"); err != nil {
		t.Fatal(err)
	}
	if err := verifyDatasetAddress(path, testDatasetAddress); err == nil {
		t.Fatal("mismatched dataset address unexpectedly accepted")
	}
}
