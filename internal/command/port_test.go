package command

import (
	"strings"
	"testing"
)

func TestCanonicalHashIsUnambiguous(t *testing.T) {
	if canonicalHash("ab", "c") == canonicalHash("a", "bc") {
		t.Fatal("canonical command hash is ambiguous")
	}
	if len(canonicalHash("actor", strings.Repeat("x", 10))) != 64 {
		t.Fatal("canonical command hash is not SHA-256 hex")
	}
}
