package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const modulePath = "github.com/05allan1213/CloudOps-Copilot"

func TestAPIBootstrapHasNoLegacyWorkerLoop(t *testing.T) {
	root := repositoryRoot(t)
	for _, path := range []string{
		filepath.Join(root, "internal", "bootstrap", "api", "api.go"),
		filepath.Join(root, "cmd", "cloudops-api", "main.go"),
	} {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"agentruntime", "remediationservice", "deliveryverification", "InitAgentRuntime", "InitRemediation", "InitDeliveryVerification"} {
			if strings.Contains(string(source), forbidden) {
				t.Fatalf("%s contains forbidden API worker dependency %q", path, forbidden)
			}
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "Start" {
				t.Errorf("%s starts a background loop", path)
			}
			return true
		})
	}

	typeOfAPI := reflect.TypeOf(API{})
	for index := 0; index < typeOfAPI.NumField(); index++ {
		name := strings.ToLower(typeOfAPI.Field(index).Name)
		if strings.Contains(name, "worker") || strings.Contains(name, "loop") {
			t.Fatalf("API owns worker field %q", typeOfAPI.Field(index).Name)
		}
	}
}

func TestAPIBootstrapDoesNotEnableRedisOrKafka(t *testing.T) {
	root := repositoryRoot(t)
	path := filepath.Join(root, "internal", "bootstrap", "api", "api.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"EnableRedis: true", "EnableKafka: true", "initKafkaProducer", "rediscache.NewClient"} {
		if strings.Contains(string(source), forbidden) {
			t.Fatalf("cloudops-api initializes forbidden legacy dependency %q", forbidden)
		}
	}
}

func TestAPICompileDependencyBoundary(t *testing.T) {
	root := repositoryRoot(t)
	output := runCommand(t, root, "go", "list", "-deps", "./cmd/cloudops-api")
	forbiddenExact := map[string]struct{}{
		modulePath + "/internal/bootstrap": {},
		modulePath + "/internal/migration": {},
		modulePath + "/migrations":         {},
	}
	forbiddenTrees := []string{
		modulePath + "/internal/startup/legacyworker",
		modulePath + "/internal/service/agentruntime",
		modulePath + "/internal/service/remediation",
		modulePath + "/internal/service/deliveryverification",
		modulePath + "/internal/agent/graph",
		modulePath + "/internal/agent/llm",
		modulePath + "/internal/infra/githubwrite",
		modulePath + "/internal/infra/observabilityread",
	}
	for _, dependency := range strings.Fields(output) {
		if _, forbidden := forbiddenExact[dependency]; forbidden {
			t.Errorf("cloudops-api compiles forbidden dependency %s", dependency)
			continue
		}
		for _, forbidden := range forbiddenTrees {
			if dependency == forbidden || strings.HasPrefix(dependency, forbidden+"/") {
				t.Errorf("cloudops-api compiles forbidden dependency %s", dependency)
			}
		}
	}
}

func TestAPILinkedSymbolBoundary(t *testing.T) {
	root := repositoryRoot(t)
	binary := filepath.Join(t.TempDir(), "cloudops-api")
	runCommand(t, root, "go", "build", "-o", binary, "./cmd/cloudops-api")
	symbols := runCommand(t, root, "go", "tool", "nm", binary)
	for _, forbidden := range []string{
		modulePath + "/internal/startup/legacyworker.",
		modulePath + "/internal/service/agentruntime.(*Worker).Start",
		modulePath + "/internal/service/remediation.(*Worker).Start",
		modulePath + "/internal/service/deliveryverification.(*Worker).Start",
		modulePath + "/internal/agent/llm.",
		modulePath + "/internal/infra/githubwrite.",
		modulePath + "/internal/infra/observabilityread.",
		modulePath + "/internal/migration.",
	} {
		if strings.Contains(symbols, forbidden) {
			t.Errorf("cloudops-api links forbidden symbol prefix %s", forbidden)
		}
	}
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func runCommand(t *testing.T, directory, name string, args ...string) string {
	t.Helper()
	command := exec.Command(name, args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", name, strings.Join(args, " "), err, output)
	}
	return string(output)
}
