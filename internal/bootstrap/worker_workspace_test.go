package bootstrap

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestWorkspaceDiagnosisOutputBudgetIsIndependentFromGeneralModel(t *testing.T) {
	general := workspaceModelMaxTokens(4096, 0)
	diagnosis := workspaceModelMaxTokens(4096, workspaceDiagnosisMaxTokens)
	if general != 4096 || diagnosis != 2048 {
		t.Fatalf("general max tokens=%d diagnosis max tokens=%d", general, diagnosis)
	}

	smaller := workspaceModelMaxTokens(512, workspaceDiagnosisMaxTokens)
	if smaller != 512 {
		t.Fatalf("smaller configured diagnosis budget=%d want=512", smaller)
	}
}

func TestWorkspaceLLMTimeoutHonorsValidatedProviderValue(t *testing.T) {
	if got := workspaceLLMTimeout(180_000); got != 3*time.Minute {
		t.Fatalf("workspace LLM timeout=%s want=3m", got)
	}
	if got := workspaceLLMTimeout(300_000); got != 5*time.Minute {
		t.Fatalf("maximum workspace LLM timeout=%s want=5m", got)
	}
	for _, invalid := range []int{0, 300_001} {
		if got := workspaceLLMTimeout(invalid); got != time.Minute {
			t.Fatalf("invalid timeout %d resolved to %s want=1m", invalid, got)
		}
	}
}

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
