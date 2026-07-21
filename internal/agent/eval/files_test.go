package agenteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent"
)

func TestLoadersUseStrictJSON(t *testing.T) {
	tests := []struct {
		name  string
		valid string
		load  func(string) error
	}{
		{
			name:  "dataset",
			valid: `{"schema_version":1,"dataset_id":"eval-v1","cases":[{"id":"case-1","mode":"model"}]}`,
			load: func(path string) error {
				_, err := LoadDataset(path)
				return err
			},
		},
		{
			name:  "oracle",
			valid: `{"schema_version":1,"dataset_id":"eval-v1","cases":{"case-1":{"expected_outcome":"diagnosed","max_tool_calls":2}}}`,
			load: func(path string) error {
				_, err := LoadOracle(path)
				return err
			},
		},
		{
			name:  "split",
			valid: `{"schema_version":1,"dataset_id":"eval-v1","calibration":["case-1"],"quality":[],"guardrail":[],"repetitions":3,"aggregation":"majority"}`,
			load: func(path string) error {
				_, err := LoadSplit(path)
				return err
			},
		},
		{
			name:  "metric spec",
			valid: `{"schema_version":1,"dataset_id":"eval-v1","metrics":[{"name":"root_cause_accuracy","numerator":"correct","denominator":"diagnosed","direction":"higher"}],"safety_zero_gate":["write_tool"]}`,
			load: func(path string) error {
				_, err := LoadMetricSpec(path)
				return err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := writeEvalFile(t, test.name+".json", []byte(test.valid))
			if err := test.load(path); err != nil {
				t.Fatalf("valid JSON was rejected: %v", err)
			}

			unknown := strings.TrimSuffix(test.valid, "}") + `,"unknown_field":true}`
			path = writeEvalFile(t, test.name+"-unknown.json", []byte(unknown))
			if err := test.load(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
				t.Fatalf("unknown field error = %v", err)
			}

			path = writeEvalFile(t, test.name+"-trailing.json", []byte(test.valid+` {}`))
			if err := test.load(path); err == nil || !strings.Contains(err.Error(), "trailing JSON data") {
				t.Fatalf("trailing data error = %v", err)
			}
		})
	}
}

func TestLoadDatasetRejectsUnknownNestedFields(t *testing.T) {
	path := writeEvalFile(t, "dataset.json", []byte(`{
  "schema_version": 1,
  "dataset_id": "eval-v1",
  "cases": [{"id": "case-1", "mode": "model", "unexpected": true}]
}`))
	if _, err := LoadDataset(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("LoadDataset() error = %v", err)
	}
}

func TestValidateEvaluationCoverageAndModes(t *testing.T) {
	dataset, oracle, split, metrics := validEvaluationFiles()
	if err := ValidateEvaluation(dataset, oracle, split, metrics); err != nil {
		t.Fatalf("ValidateEvaluation() error = %v", err)
	}

	t.Run("duplicate dataset case", func(t *testing.T) {
		invalid := dataset
		invalid.Cases = append(append([]EvalCase(nil), dataset.Cases...), dataset.Cases[0])
		if err := ValidateEvaluation(invalid, oracle, split, metrics); err == nil || !strings.Contains(err.Error(), "duplicate case ID") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})

	t.Run("dataset ID mismatch", func(t *testing.T) {
		invalid := metrics
		invalid.DatasetID = "other"
		if err := ValidateEvaluation(dataset, oracle, split, invalid); err == nil || !strings.Contains(err.Error(), "dataset ID mismatch") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})

	t.Run("oracle coverage", func(t *testing.T) {
		invalid := oracle
		invalid.Cases = cloneOracles(oracle.Cases)
		delete(invalid.Cases, "guard-1")
		if err := ValidateEvaluation(dataset, invalid, split, metrics); err == nil || !strings.Contains(err.Error(), "oracle missing case") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})

	t.Run("split coverage", func(t *testing.T) {
		invalid := split
		invalid.Quality = nil
		if err := ValidateEvaluation(dataset, oracle, invalid, metrics); err == nil || !strings.Contains(err.Error(), "split missing case") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})

	t.Run("guardrail reference mode", func(t *testing.T) {
		invalid := split
		invalid.Guardrail = []string{"quality-1"}
		invalid.Quality = []string{"guard-1"}
		if err := ValidateEvaluation(dataset, oracle, invalid, metrics); err == nil || !strings.Contains(err.Error(), "has mode") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})

	t.Run("minimum repetitions", func(t *testing.T) {
		invalid := split
		invalid.Repetitions = 2
		if err := ValidateEvaluation(dataset, oracle, invalid, metrics); err == nil || !strings.Contains(err.Error(), "at least 3") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})

	t.Run("aggregation required", func(t *testing.T) {
		invalid := split
		invalid.Aggregation = " "
		if err := ValidateEvaluation(dataset, oracle, invalid, metrics); err == nil || !strings.Contains(err.Error(), "aggregation") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})

	t.Run("metrics required", func(t *testing.T) {
		invalid := metrics
		invalid.Metrics = nil
		if err := ValidateEvaluation(dataset, oracle, split, invalid); err == nil || !strings.Contains(err.Error(), "metrics must not be empty") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})

	t.Run("safety required", func(t *testing.T) {
		invalid := metrics
		invalid.Safety = nil
		if err := ValidateEvaluation(dataset, oracle, split, invalid); err == nil || !strings.Contains(err.Error(), "safety_zero_gate") {
			t.Fatalf("ValidateEvaluation() error = %v", err)
		}
	})
}

func TestCanonicalAndFileSHA256(t *testing.T) {
	canonical, err := CanonicalJSONSHA256(map[string]int{"b": 2, "a": 1})
	if err != nil {
		t.Fatalf("CanonicalJSONSHA256() error = %v", err)
	}
	if want := SHA256Bytes([]byte(`{"a":1,"b":2}`)); canonical != want {
		t.Fatalf("CanonicalJSONSHA256() = %q, want %q", canonical, want)
	}

	path := writeEvalFile(t, "exact.txt", []byte("exact bytes\n"))
	got, err := FileSHA256(path)
	if err != nil {
		t.Fatalf("FileSHA256() error = %v", err)
	}
	if want := SHA256Bytes([]byte("exact bytes\n")); got != want {
		t.Fatalf("FileSHA256() = %q, want %q", got, want)
	}
	withoutNewline := writeEvalFile(t, "without-newline.txt", []byte("exact bytes"))
	second, err := FileSHA256(withoutNewline)
	if err != nil {
		t.Fatalf("FileSHA256() error = %v", err)
	}
	if second == got {
		t.Fatal("FileSHA256() normalized the trailing newline")
	}
}

func TestBuildManifestHashesExactMaterialWithoutIncludingIt(t *testing.T) {
	dataset := []byte("dataset bytes\n")
	oracle := []byte("oracle bytes\n")
	split := []byte("split bytes\n")
	metrics := []byte("metric bytes\n")
	reducer := []byte("package agent\n// reducer SECRET_CANARY\n")
	runner := []byte("package agenteval\n// runner\n")
	prompt := []byte("SYSTEM PROMPT\nSECRET_CANARY=prompt-only\n")

	datasetPath := writeEvalFile(t, "dataset.json", dataset)
	oraclePath := writeEvalFile(t, "oracle.json", oracle)
	splitPath := writeEvalFile(t, "split.json", split)
	metricPath := writeEvalFile(t, "metrics.json", metrics)
	reducerPath := writeEvalFile(t, "reducer.go", reducer)
	runnerPath := writeEvalFile(t, "runner.go", runner)
	contracts := Contracts{
		SchemaVersion: DatasetSchemaVersion,
		DatasetID:     "eval-v1",
		Actions: []agent.ModelActionSchema{{
			Tool: "inspect_workload", TemplateIDs: []string{"v1"}, ParameterKeys: []string{"workload"},
		}},
	}
	createdAt := time.Date(2026, 7, 21, 18, 30, 0, 123456789, time.FixedZone("CST", 8*60*60))

	manifest, err := BuildManifest(
		"eval-v1", datasetPath, oraclePath, splitPath, metricPath,
		contracts, prompt, reducerPath, runnerPath, createdAt,
	)
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}
	contractsHash, err := CanonicalJSONSHA256(contracts)
	if err != nil {
		t.Fatalf("CanonicalJSONSHA256(contracts) error = %v", err)
	}
	want := Manifest{
		SchemaVersion:        ManifestSchemaVersion,
		DatasetID:            "eval-v1",
		DatasetSHA256:        SHA256Bytes(dataset),
		OracleSHA256:         SHA256Bytes(oracle),
		SplitSHA256:          SHA256Bytes(split),
		MetricSpecSHA256:     SHA256Bytes(metrics),
		ContractsSHA256:      contractsHash,
		PromptMaterialSHA256: SHA256Bytes(prompt),
		ReducerSourceSHA256:  SHA256Bytes(reducer),
		RunnerSourceSHA256:   SHA256Bytes(runner),
		CreatedAt:            createdAt.UTC().Format(time.RFC3339Nano),
	}
	if manifest != want {
		t.Fatalf("BuildManifest() = %#v, want %#v", manifest, want)
	}

	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("json.Marshal(manifest) error = %v", err)
	}
	for _, forbidden := range []string{"SECRET_CANARY", "SYSTEM PROMPT", reducerPath, runnerPath} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("manifest exposed raw material %q: %s", forbidden, encoded)
		}
	}
}

func TestBuildManifestRejectsInvalidBindingsAndEmptyMaterial(t *testing.T) {
	nonEmpty := writeEvalFile(t, "non-empty", []byte("x"))
	empty := writeEvalFile(t, "empty", nil)
	contracts := Contracts{
		SchemaVersion: DatasetSchemaVersion,
		DatasetID:     "eval-v1",
		Actions:       []agent.ModelActionSchema{{Tool: "inspect_workload"}},
	}
	now := time.Now()

	if _, err := BuildManifest("other", nonEmpty, nonEmpty, nonEmpty, nonEmpty, contracts, []byte("prompt"), nonEmpty, nonEmpty, now); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("dataset binding error = %v", err)
	}
	if _, err := BuildManifest("eval-v1", nonEmpty, nonEmpty, nonEmpty, nonEmpty, contracts, nil, nonEmpty, nonEmpty, now); err == nil || !strings.Contains(err.Error(), "prompt material") {
		t.Fatalf("empty prompt error = %v", err)
	}
	if _, err := BuildManifest("eval-v1", nonEmpty, nonEmpty, nonEmpty, nonEmpty, contracts, []byte("prompt"), empty, nonEmpty, now); err == nil || !strings.Contains(err.Error(), "reducer source") {
		t.Fatalf("empty reducer source error = %v", err)
	}
}

func validEvaluationFiles() (Dataset, Oracle, Split, MetricSpec) {
	dataset := Dataset{
		SchemaVersion: DatasetSchemaVersion,
		DatasetID:     "eval-v1",
		Cases: []EvalCase{
			{ID: "calibration-1", Mode: ModeModel},
			{ID: "quality-1", Mode: ModeModel},
			{ID: "guard-1", Mode: ModeGuardrail},
		},
	}
	oracle := Oracle{
		SchemaVersion: OracleSchemaVersion,
		DatasetID:     "eval-v1",
		Cases: map[string]CaseOracle{
			"calibration-1": {ExpectedOutcome: OutcomeDiagnosed, MaxToolCalls: 3},
			"quality-1":     {ExpectedOutcome: OutcomeInsufficient, MaxToolCalls: 3},
			"guard-1":       {ExpectedOutcome: OutcomeInsufficient},
		},
	}
	split := Split{
		SchemaVersion: SplitSchemaVersion,
		DatasetID:     "eval-v1",
		Calibration:   []string{"calibration-1"},
		Quality:       []string{"quality-1"},
		Guardrail:     []string{"guard-1"},
		Repetitions:   3,
		Aggregation:   "majority",
	}
	metrics := MetricSpec{
		SchemaVersion: MetricSchemaVersion,
		DatasetID:     "eval-v1",
		Metrics: []MetricDefinition{{
			Name: "root_cause_accuracy", Numerator: "correct", Denominator: "diagnosed", Direction: "higher",
		}},
		Safety: []string{"write_tool", "scope_escape"},
	}
	return dataset, oracle, split, metrics
}

func cloneOracles(source map[string]CaseOracle) map[string]CaseOracle {
	result := make(map[string]CaseOracle, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

func writeEvalFile(t *testing.T, name string, value []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), strings.ReplaceAll(name, " ", "-"))
	if err := os.WriteFile(path, value, 0o600); err != nil {
		t.Fatalf("os.WriteFile(%q) error = %v", path, err)
	}
	return path
}
