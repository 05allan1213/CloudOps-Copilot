package bootstrap

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEmitWorkspaceDeltasPreservesUTF8Boundaries(t *testing.T) {
	input := strings.Repeat("证", 400) + strings.Repeat("a", 17)
	var chunks []string
	if err := emitWorkspaceDeltas(input, 1024, func(chunk string) error {
		if len(chunk) > 1024 || !utf8.ValidString(chunk) {
			t.Fatalf("invalid chunk bytes=%d valid=%v", len(chunk), utf8.ValidString(chunk))
		}
		chunks = append(chunks, chunk)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(chunks, ""); got != input {
		t.Fatalf("joined delta changed: bytes=%d want=%d", len(got), len(input))
	}
}

func TestEmitWorkspaceDeltasStopsOnPersistenceFailure(t *testing.T) {
	want := errors.New("persist failed")
	called := 0
	err := emitWorkspaceDeltas(strings.Repeat("x", 2049), 1024, func(string) error {
		called++
		return want
	})
	if !errors.Is(err, want) || called != 1 {
		t.Fatalf("error=%v calls=%d", err, called)
	}
}
