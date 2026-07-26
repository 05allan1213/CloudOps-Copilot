package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/05allan1213/CloudOps-Copilot/internal/agent/adapter"
	eval "github.com/05allan1213/CloudOps-Copilot/internal/agent/eval"
	"github.com/05allan1213/CloudOps-Copilot/internal/agent/llm"
)

type options struct {
	mode        string
	root        string
	dataset     string
	out         string
	report      string
	baseline    string
	thresholds  string
	onlySplit   string
	caseIDs     string
	repetitions int
	timeout     time.Duration
	freezeAt    string
}

func main() {
	var value options
	flag.StringVar(&value.mode, "mode", "validate", "freeze, validate, baseline, guardrail, model, or gate")
	flag.StringVar(&value.root, "root", ".", "repository root")
	flag.StringVar(&value.dataset, "dataset", "", "dataset SHA-256; defaults to the address in eval/index.json")
	flag.StringVar(&value.out, "out", "", "report output path; stdout when empty")
	flag.StringVar(&value.report, "report", "", "measured model report required by gate mode")
	flag.StringVar(&value.baseline, "baseline", "", "fixed-pipeline baseline report required by gate mode")
	flag.StringVar(&value.thresholds, "thresholds", "", "quality thresholds path; defaults to the selected revision")
	flag.StringVar(&value.onlySplit, "split", "model", "model, calibration, quality, guardrail, or all")
	flag.StringVar(&value.caseIDs, "cases", "", "comma-separated case IDs")
	flag.IntVar(&value.repetitions, "repetitions", 0, "override repetitions")
	flag.DurationVar(&value.timeout, "timeout", 90*time.Second, "per-case timeout")
	flag.StringVar(&value.freezeAt, "freeze-at", "", "RFC3339 timestamp required by freeze mode")
	flag.Parse()
	if err := run(value); err != nil {
		fmt.Fprintln(os.Stderr, "agent-eval:", err)
		os.Exit(1)
	}
}

func run(options options) error {
	paths, err := evalPaths(options.root, options.dataset)
	if err != nil {
		return err
	}
	if err := verifyDatasetAddress(paths.dataset, paths.datasetAddress); err != nil {
		return err
	}
	dataset, err := eval.LoadDataset(paths.dataset)
	if err != nil {
		return err
	}
	oracle, err := eval.LoadOracle(paths.oracle)
	if err != nil {
		return err
	}
	split, err := eval.LoadSplit(paths.split)
	if err != nil {
		return err
	}
	metrics, err := eval.LoadMetricSpec(paths.metrics)
	if err != nil {
		return err
	}
	if err := eval.ValidateEvaluation(dataset, oracle, split, metrics); err != nil {
		return err
	}
	contracts := eval.EvaluationContracts(dataset.DatasetID)
	promptMaterial := adapter.StructuredPromptMaterial()
	if options.mode == "freeze" {
		if strings.TrimSpace(options.out) == "" {
			return errors.New("freeze mode requires -out")
		}
		createdAt, parseErr := time.Parse(time.RFC3339Nano, strings.TrimSpace(options.freezeAt))
		if parseErr != nil {
			return fmt.Errorf("freeze mode requires a valid -freeze-at: %w", parseErr)
		}
		manifest, buildErr := eval.BuildRuntimeManifest(dataset.DatasetID, paths.dataset, paths.oracle, paths.split, paths.metrics, contracts, promptMaterial, paths.reducer, paths.runner, paths.runtimeSources, createdAt)
		if buildErr != nil {
			return buildErr
		}
		return writeReport(options.out, manifest)
	}
	manifest, err := eval.LoadManifest(paths.manifest)
	if err != nil {
		return err
	}
	if manifest.DatasetID != dataset.DatasetID {
		return errors.New("agent eval manifest dataset ID does not match the loaded dataset")
	}
	if err := eval.VerifyManifest(manifest, paths.dataset, paths.oracle, paths.split, paths.metrics, contracts, promptMaterial, paths.reducer, paths.runner, paths.runtimeSources); err != nil {
		return err
	}
	caseIDs := splitCaseIDs(options.caseIDs)
	runOptions := eval.RunOptions{OnlySplit: options.onlySplit, CaseIDs: caseIDs, Repetitions: options.repetitions, Timeout: options.timeout}
	var report any
	switch options.mode {
	case "validate":
		report = struct {
			Status   string        `json:"status"`
			Dataset  string        `json:"dataset_id"`
			Cases    int           `json:"cases"`
			Manifest eval.Manifest `json:"manifest"`
		}{Status: "PASS", Dataset: dataset.DatasetID, Cases: len(dataset.Cases), Manifest: manifest}
	case "baseline":
		result, runErr := eval.RunFixedBaseline(dataset, oracle, split, manifest, runOptions)
		if runErr != nil {
			return runErr
		}
		report = result
	case "guardrail":
		result, runErr := eval.RunGuardrails(context.Background(), dataset, oracle, split, manifest)
		if runErr != nil {
			return runErr
		}
		report = result
	case "model":
		model, provider, modelName, available, modelErr := loadModel()
		if modelErr != nil {
			return modelErr
		}
		if !available {
			report = eval.Report{SchemaVersion: 1, DatasetID: dataset.DatasetID, Manifest: manifest, Status: "NOT_RUN", Provider: provider, Model: modelName, Repetitions: split.Repetitions, Aggregation: split.Aggregation, GeneratedAt: time.Now().UTC(), Note: "Real model credentials are not configured; no provider call was made."}
			break
		}
		result, runErr := eval.RunModel(context.Background(), dataset, oracle, split, manifest, model, runOptionsWithIdentity(runOptions, provider, modelName))
		if runErr != nil {
			return runErr
		}
		report = result
	case "gate":
		result, gateErr := runQualityGate(paths, manifest, split, options)
		if gateErr != nil {
			return gateErr
		}
		report = result
	default:
		return fmt.Errorf("unsupported mode %q", options.mode)
	}
	return writeReport(options.out, report)
}

type evalPathsValue struct {
	datasetAddress                                                         string
	dataset, oracle, split, metrics, manifest, thresholds, reducer, runner string
	runtimeSources                                                         []string
}

type datasetIndex struct {
	SchemaVersion       int    `json:"schema_version"`
	ActiveDatasetSHA256 string `json:"active_dataset_sha256"`
}

func evalPaths(root, address string) (evalPathsValue, error) {
	address = strings.TrimSpace(address)
	if address == "" {
		indexPath := filepath.Join(root, "eval", "index.json")
		encoded, err := os.ReadFile(indexPath)
		if err != nil {
			return evalPathsValue{}, fmt.Errorf("read Agent Eval index: %w", err)
		}
		var index datasetIndex
		if err := json.Unmarshal(encoded, &index); err != nil {
			return evalPathsValue{}, fmt.Errorf("decode Agent Eval index: %w", err)
		}
		if index.SchemaVersion != 1 {
			return evalPathsValue{}, fmt.Errorf("unsupported Agent Eval index schema version %d", index.SchemaVersion)
		}
		address = strings.TrimSpace(index.ActiveDatasetSHA256)
	}
	if !validDatasetAddress(address) {
		return evalPathsValue{}, fmt.Errorf("invalid dataset SHA-256 %q", address)
	}
	directory := filepath.Join(root, "eval", "sha256-"+address)
	paths := evalPathsValue{
		datasetAddress: address,
		dataset:        filepath.Join(directory, "dataset.json"),
		oracle:         filepath.Join(directory, "oracle.json"),
		split:          filepath.Join(directory, "split.json"),
		metrics:        filepath.Join(directory, "metrics.json"),
		manifest:       filepath.Join(directory, "manifest.json"),
		thresholds:     filepath.Join(directory, "thresholds.json"),
		reducer:        filepath.Join(root, "internal", "agent", "state_delta.go"),
		runner:         filepath.Join(root, "internal", "agent"),
	}
	paths.runtimeSources = []string{
		filepath.Join(root, "internal", "taskhandler", "investigation_start.go"),
		filepath.Join(root, "internal", "taskhandler", "investigation_step.go"),
		filepath.Join(root, "internal", "taskhandler", "registry.go"),
		filepath.Join(root, "internal", "bootstrap", "worker_operations.go"),
		filepath.Join(root, "internal", "bootstrap", "worker_provider.go"),
		filepath.Join(root, "internal", "config", "config.go"),
		filepath.Join(root, "migrations", "00001_cloudops_baseline.sql"),
	}
	return paths, nil
}

func validDatasetAddress(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func verifyDatasetAddress(path, expected string) error {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read content-addressed Agent Eval dataset: %w", err)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(encoded))
	if actual != expected {
		return fmt.Errorf("Agent Eval dataset address mismatch: expected sha256:%s, got sha256:%s", expected, actual)
	}
	return nil
}

func splitCaseIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func runOptionsWithIdentity(value eval.RunOptions, provider, model string) eval.RunOptions {
	value.Provider, value.Model = provider, model
	return value
}

func loadModel() (*adapter.LLMModel, string, string, bool, error) {
	key := strings.TrimSpace(os.Getenv("LLM_API_KEY"))
	if key == "" {
		if path := strings.TrimSpace(os.Getenv("LLM_API_KEY_FILE")); path != "" {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, "", "", false, fmt.Errorf("read configured model key file: %w", err)
			}
			key = strings.TrimSpace(string(data))
		}
	}
	provider := strings.TrimSpace(os.Getenv("LLM_PROVIDER_NAME"))
	if provider == "" {
		provider = "configured-llm"
	}
	modelName := strings.TrimSpace(os.Getenv("LLM_MODEL"))
	if modelName == "" {
		modelName = strings.TrimSpace(os.Getenv("LLM_MODEL"))
	}
	if modelName == "" {
		modelName = "deepseek-chat"
	}
	if key == "" {
		return nil, provider, modelName, false, nil
	}
	apiURL := strings.TrimSpace(os.Getenv("LLM_API_URL"))
	if apiURL == "" {
		apiURL = "https://api.deepseek.com/chat/completions"
	}
	timeout := 60 * time.Second
	if raw := strings.TrimSpace(os.Getenv("LLM_TIMEOUT_SECONDS")); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil || seconds <= 0 {
			return nil, provider, modelName, false, errors.New("LLM_TIMEOUT_SECONDS must be a positive integer")
		}
		timeout = time.Duration(seconds) * time.Second
	}
	zeroRetries := 0
	client := llm.NewClient(llm.Options{APIKey: key, APIURL: apiURL, Model: modelName, Timeout: timeout, MaxTokens: 4096, MaxRetries: &zeroRetries})
	model, err := adapter.NewLLMModel(client)
	if err != nil {
		return nil, provider, modelName, false, err
	}
	return model, provider, modelName, true, nil
}

func writeReport(path string, report any) error {
	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if strings.TrimSpace(path) == "" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
