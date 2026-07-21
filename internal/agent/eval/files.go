package agenteval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LoadDataset reads and validates one frozen dataset file. JSON decoding is
// deliberately strict: unknown fields and a second top-level value are both
// rejected so an accidental edit cannot be silently ignored.
func LoadDataset(path string) (Dataset, error) {
	var value Dataset
	if err := decodeStrictJSONFile(path, &value); err != nil {
		return Dataset{}, fmt.Errorf("load dataset: %w", err)
	}
	if err := ValidateDataset(value); err != nil {
		return Dataset{}, err
	}
	return value, nil
}

// LoadOracle reads and validates one frozen oracle file.
func LoadOracle(path string) (Oracle, error) {
	var value Oracle
	if err := decodeStrictJSONFile(path, &value); err != nil {
		return Oracle{}, fmt.Errorf("load oracle: %w", err)
	}
	if err := ValidateOracle(value); err != nil {
		return Oracle{}, err
	}
	return value, nil
}

// LoadSplit reads and validates one frozen split file.
func LoadSplit(path string) (Split, error) {
	var value Split
	if err := decodeStrictJSONFile(path, &value); err != nil {
		return Split{}, fmt.Errorf("load split: %w", err)
	}
	if err := ValidateSplit(value); err != nil {
		return Split{}, err
	}
	return value, nil
}

// LoadMetricSpec reads and validates one frozen metric specification file.
func LoadMetricSpec(path string) (MetricSpec, error) {
	var value MetricSpec
	if err := decodeStrictJSONFile(path, &value); err != nil {
		return MetricSpec{}, fmt.Errorf("load metric spec: %w", err)
	}
	if err := ValidateMetricSpec(value); err != nil {
		return MetricSpec{}, err
	}
	return value, nil
}

// LoadManifest reads the committed freeze record. It is verified against all
// live frozen files and source material by VerifyManifest.
func LoadManifest(path string) (Manifest, error) {
	var value Manifest
	if err := decodeStrictJSONFile(path, &value); err != nil {
		return Manifest{}, fmt.Errorf("load manifest: %w", err)
	}
	if err := value.Validate(); err != nil {
		return Manifest{}, err
	}
	return value, nil
}

// VerifyManifest recomputes the manifest using its committed created_at and
// rejects any dataset, oracle, split, metric, contract, prompt, reducer, or
// runner drift.
func VerifyManifest(
	expected Manifest,
	datasetPath, oraclePath, splitPath, metricSpecPath string,
	contracts Contracts,
	promptMaterial []byte,
	reducerSourcePath, runnerSourcePath string,
	runtimeSourcePaths []string,
) error {
	if err := expected.Validate(); err != nil {
		return err
	}
	createdAt, err := time.Parse(time.RFC3339Nano, expected.CreatedAt)
	if err != nil {
		return err
	}
	var actual Manifest
	if expected.SchemaVersion == ManifestSchemaVersion {
		actual, err = BuildRuntimeManifest(expected.DatasetID, datasetPath, oraclePath, splitPath, metricSpecPath, contracts, promptMaterial, reducerSourcePath, runnerSourcePath, runtimeSourcePaths, createdAt)
	} else {
		actual, err = BuildManifest(expected.DatasetID, datasetPath, oraclePath, splitPath, metricSpecPath, contracts, promptMaterial, reducerSourcePath, runnerSourcePath, createdAt)
	}
	if err != nil {
		return err
	}
	if actual != expected {
		return fmt.Errorf("Agent Eval manifest drift: expected=%+v actual=%+v", expected, actual)
	}
	return nil
}

// ValidateDataset validates constraints that do not depend on the other
// frozen files.
func ValidateDataset(dataset Dataset) error {
	if dataset.SchemaVersion != DatasetSchemaVersion {
		return fmt.Errorf("dataset: unsupported schema version %d", dataset.SchemaVersion)
	}
	if err := requireDatasetID("dataset", dataset.DatasetID); err != nil {
		return err
	}
	if len(dataset.Cases) == 0 {
		return errors.New("dataset: cases must not be empty")
	}
	seen := make(map[string]struct{}, len(dataset.Cases))
	for index, item := range dataset.Cases {
		if strings.TrimSpace(item.ID) == "" {
			return fmt.Errorf("dataset: case %d has empty ID", index)
		}
		if _, ok := seen[item.ID]; ok {
			return fmt.Errorf("dataset: duplicate case ID %q", item.ID)
		}
		seen[item.ID] = struct{}{}
		switch item.Mode {
		case ModeModel, ModeGuardrail:
		default:
			return fmt.Errorf("dataset: case %q has invalid mode %q", item.ID, item.Mode)
		}
	}
	return nil
}

// ValidateOracle validates an oracle independently of a dataset. Coverage
// and dataset identity are checked by ValidateEvaluation.
func ValidateOracle(oracle Oracle) error {
	if oracle.SchemaVersion != OracleSchemaVersion {
		return fmt.Errorf("oracle: unsupported schema version %d", oracle.SchemaVersion)
	}
	if err := requireDatasetID("oracle", oracle.DatasetID); err != nil {
		return err
	}
	if len(oracle.Cases) == 0 {
		return errors.New("oracle: cases must not be empty")
	}
	for caseID, item := range oracle.Cases {
		if strings.TrimSpace(caseID) == "" {
			return errors.New("oracle: case ID must not be empty")
		}
		switch item.ExpectedOutcome {
		case OutcomeDiagnosed, OutcomeInsufficient:
		default:
			return fmt.Errorf("oracle: case %q has invalid expected outcome %q", caseID, item.ExpectedOutcome)
		}
		if item.MaxToolCalls < 0 {
			return fmt.Errorf("oracle: case %q has negative max tool calls", caseID)
		}
		if item.BaselineMaxToolCalls < 0 {
			return fmt.Errorf("oracle: case %q has negative baseline max tool calls", caseID)
		}
	}
	return nil
}

// ValidateSplit validates split-local uniqueness and the minimum repetition
// contract. Cross-file membership and mode checks are done by
// ValidateEvaluation.
func ValidateSplit(split Split) error {
	if split.SchemaVersion != SplitSchemaVersion {
		return fmt.Errorf("split: unsupported schema version %d", split.SchemaVersion)
	}
	if err := requireDatasetID("split", split.DatasetID); err != nil {
		return err
	}
	if split.Repetitions < 3 {
		return fmt.Errorf("split: repetitions must be at least 3, got %d", split.Repetitions)
	}
	if strings.TrimSpace(split.Aggregation) == "" {
		return errors.New("split: aggregation must not be empty")
	}
	seen := make(map[string]string)
	groups := []struct {
		name string
		IDs  []string
	}{
		{name: SplitCalibration, IDs: split.Calibration},
		{name: SplitQuality, IDs: split.Quality},
		{name: ModeGuardrail, IDs: split.Guardrail},
	}
	for _, group := range groups {
		name, IDs := group.name, group.IDs
		for _, caseID := range IDs {
			if strings.TrimSpace(caseID) == "" {
				return fmt.Errorf("split: %s contains an empty case ID", name)
			}
			if previous, ok := seen[caseID]; ok {
				return fmt.Errorf("split: case %q appears in both %s and %s", caseID, previous, name)
			}
			seen[caseID] = name
		}
	}
	if len(seen) == 0 {
		return errors.New("split: case references must not be empty")
	}
	return nil
}

// ValidateMetricSpec validates that at least one metric and one safety gate
// are frozen, and that each named metric has a complete definition.
func ValidateMetricSpec(spec MetricSpec) error {
	if spec.SchemaVersion != MetricSchemaVersion {
		return fmt.Errorf("metric spec: unsupported schema version %d", spec.SchemaVersion)
	}
	if err := requireDatasetID("metric spec", spec.DatasetID); err != nil {
		return err
	}
	if len(spec.Metrics) == 0 {
		return errors.New("metric spec: metrics must not be empty")
	}
	seen := make(map[string]struct{}, len(spec.Metrics))
	for index, metric := range spec.Metrics {
		if strings.TrimSpace(metric.Name) == "" {
			return fmt.Errorf("metric spec: metric %d has empty name", index)
		}
		if _, ok := seen[metric.Name]; ok {
			return fmt.Errorf("metric spec: duplicate metric %q", metric.Name)
		}
		seen[metric.Name] = struct{}{}
		if strings.TrimSpace(metric.Numerator) == "" || strings.TrimSpace(metric.Denominator) == "" {
			return fmt.Errorf("metric spec: metric %q must define numerator and denominator", metric.Name)
		}
		if strings.TrimSpace(metric.Direction) == "" {
			return fmt.Errorf("metric spec: metric %q has empty direction", metric.Name)
		}
	}
	if len(spec.Safety) == 0 {
		return errors.New("metric spec: safety_zero_gate must not be empty")
	}
	seenSafety := make(map[string]struct{}, len(spec.Safety))
	for _, gate := range spec.Safety {
		if strings.TrimSpace(gate) == "" {
			return errors.New("metric spec: safety_zero_gate contains an empty entry")
		}
		if _, ok := seenSafety[gate]; ok {
			return fmt.Errorf("metric spec: duplicate safety gate %q", gate)
		}
		seenSafety[gate] = struct{}{}
	}
	return nil
}

// ValidateContracts checks the contract envelope before it is hashed into a
// manifest. It intentionally does not inspect provider credentials or raw
// prompt material.
func ValidateContracts(contracts Contracts) error {
	if contracts.SchemaVersion != DatasetSchemaVersion {
		return fmt.Errorf("contracts: unsupported schema version %d", contracts.SchemaVersion)
	}
	if err := requireDatasetID("contracts", contracts.DatasetID); err != nil {
		return err
	}
	if len(contracts.Actions) == 0 {
		return errors.New("contracts: actions must not be empty")
	}
	seen := make(map[string]struct{}, len(contracts.Actions))
	for index, action := range contracts.Actions {
		if strings.TrimSpace(action.Tool) == "" {
			return fmt.Errorf("contracts: action %d has empty tool", index)
		}
		if _, ok := seen[action.Tool]; ok {
			return fmt.Errorf("contracts: duplicate tool %q", action.Tool)
		}
		seen[action.Tool] = struct{}{}
	}
	return nil
}

// ValidateEvaluation validates all cross-file bindings. Every dataset case
// must have exactly one oracle and exactly one split membership, and split
// membership must agree with the case mode.
func ValidateEvaluation(dataset Dataset, oracle Oracle, split Split, metrics MetricSpec) error {
	if err := ValidateDataset(dataset); err != nil {
		return err
	}
	if err := ValidateOracle(oracle); err != nil {
		return err
	}
	if err := ValidateSplit(split); err != nil {
		return err
	}
	if err := ValidateMetricSpec(metrics); err != nil {
		return err
	}
	if oracle.DatasetID != dataset.DatasetID || split.DatasetID != dataset.DatasetID || metrics.DatasetID != dataset.DatasetID {
		return fmt.Errorf("evaluation: dataset ID mismatch: dataset=%q oracle=%q split=%q metric_spec=%q", dataset.DatasetID, oracle.DatasetID, split.DatasetID, metrics.DatasetID)
	}

	caseModes := make(map[string]string, len(dataset.Cases))
	for _, item := range dataset.Cases {
		caseModes[item.ID] = item.Mode
		if _, ok := oracle.Cases[item.ID]; !ok {
			return fmt.Errorf("evaluation: oracle missing case %q", item.ID)
		}
	}
	for caseID := range oracle.Cases {
		if _, ok := caseModes[caseID]; !ok {
			return fmt.Errorf("evaluation: oracle contains unknown case %q", caseID)
		}
	}

	seenSplit := make(map[string]string, len(caseModes))
	checkSplit := func(name string, IDs []string, expectedMode string) error {
		for _, caseID := range IDs {
			mode, ok := caseModes[caseID]
			if !ok {
				return fmt.Errorf("evaluation: %s references unknown case %q", name, caseID)
			}
			if mode != expectedMode {
				return fmt.Errorf("evaluation: %s case %q has mode %q, want %q", name, caseID, mode, expectedMode)
			}
			seenSplit[caseID] = name
		}
		return nil
	}
	if err := checkSplit(SplitCalibration, split.Calibration, ModeModel); err != nil {
		return err
	}
	if err := checkSplit(SplitQuality, split.Quality, ModeModel); err != nil {
		return err
	}
	if err := checkSplit(ModeGuardrail, split.Guardrail, ModeGuardrail); err != nil {
		return err
	}
	for caseID, mode := range caseModes {
		if _, ok := seenSplit[caseID]; !ok {
			return fmt.Errorf("evaluation: split missing case %q", caseID)
		}
		if mode == ModeGuardrail && seenSplit[caseID] != ModeGuardrail {
			return fmt.Errorf("evaluation: guardrail case %q must be in guardrail split", caseID)
		}
	}
	return nil
}

// SHA256Bytes returns a lowercase hexadecimal SHA-256 digest.
func SHA256Bytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

// FileSHA256 hashes the exact bytes on disk. No newline normalization or JSON
// parsing is performed.
func FileSHA256(path string) (string, error) {
	value, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read file for SHA-256: %w", err)
	}
	return SHA256Bytes(value), nil
}

// CanonicalJSON returns the deterministic JSON representation used for
// contract hashing. Re-decoding through an interface sorts object keys while
// UseNumber avoids changing large integer values.
func CanonicalJSON(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal canonical JSON: %w", err)
	}
	return canonicalizeJSON(encoded)
}

// CanonicalJSONSHA256 hashes CanonicalJSON(value).
func CanonicalJSONSHA256(value any) (string, error) {
	encoded, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	return SHA256Bytes(encoded), nil
}

// BuildManifest binds exact frozen files and source snapshots to canonical
// contracts and exact prompt material. Raw content is never copied into the
// manifest, so secrets cannot be accidentally persisted in it.
func BuildManifest(
	datasetID string,
	datasetPath string,
	oraclePath string,
	splitPath string,
	metricSpecPath string,
	contracts Contracts,
	promptMaterial []byte,
	reducerSourcePath string,
	runnerSourcePath string,
	createdAt time.Time,
) (Manifest, error) {
	return buildManifest(LegacyManifestSchemaVersion, datasetID, datasetPath, oraclePath, splitPath, metricSpecPath, contracts, promptMaterial, reducerSourcePath, runnerSourcePath, nil, createdAt)
}

// BuildRuntimeManifest adds a deterministic hash of the production
// task/bootstrap/config/migration source that binds the provider-visible Agent
// adapter to its durable AgentRun identity and recovery path.
func BuildRuntimeManifest(
	datasetID string,
	datasetPath string,
	oraclePath string,
	splitPath string,
	metricSpecPath string,
	contracts Contracts,
	promptMaterial []byte,
	reducerSourcePath string,
	runnerSourcePath string,
	runtimeSourcePaths []string,
	createdAt time.Time,
) (Manifest, error) {
	return buildManifest(ManifestSchemaVersion, datasetID, datasetPath, oraclePath, splitPath, metricSpecPath, contracts, promptMaterial, reducerSourcePath, runnerSourcePath, runtimeSourcePaths, createdAt)
}

func buildManifest(
	schemaVersion int,
	datasetID string,
	datasetPath string,
	oraclePath string,
	splitPath string,
	metricSpecPath string,
	contracts Contracts,
	promptMaterial []byte,
	reducerSourcePath string,
	runnerSourcePath string,
	runtimeSourcePaths []string,
	createdAt time.Time,
) (Manifest, error) {
	if strings.TrimSpace(datasetID) == "" {
		return Manifest{}, errors.New("manifest: dataset ID must not be empty")
	}
	if err := ValidateContracts(contracts); err != nil {
		return Manifest{}, err
	}
	if contracts.DatasetID != datasetID {
		return Manifest{}, fmt.Errorf("manifest: contracts dataset ID %q does not match %q", contracts.DatasetID, datasetID)
	}
	if len(bytes.TrimSpace(promptMaterial)) == 0 {
		return Manifest{}, errors.New("manifest: prompt material must not be empty")
	}
	if createdAt.IsZero() {
		return Manifest{}, errors.New("manifest: created_at must not be zero")
	}

	datasetHash, err := FileSHA256(datasetPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest dataset: %w", err)
	}
	oracleHash, err := FileSHA256(oraclePath)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest oracle: %w", err)
	}
	splitHash, err := FileSHA256(splitPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest split: %w", err)
	}
	metricHash, err := FileSHA256(metricSpecPath)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest metric spec: %w", err)
	}
	reducerHash, err := hashNonEmptyPath(reducerSourcePath)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest reducer source: %w", err)
	}
	runnerHash, err := hashNonEmptyPath(runnerSourcePath)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest runner source: %w", err)
	}
	runtimeHash := ""
	if schemaVersion == ManifestSchemaVersion {
		runtimeHash, err = hashNonEmptyPaths(runtimeSourcePaths)
		if err != nil {
			return Manifest{}, fmt.Errorf("manifest runtime source: %w", err)
		}
	}
	contractsHash, err := CanonicalJSONSHA256(contracts)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest contracts: %w", err)
	}

	return Manifest{
		SchemaVersion:        schemaVersion,
		DatasetID:            datasetID,
		DatasetSHA256:        datasetHash,
		OracleSHA256:         oracleHash,
		SplitSHA256:          splitHash,
		MetricSpecSHA256:     metricHash,
		ContractsSHA256:      contractsHash,
		PromptMaterialSHA256: SHA256Bytes(promptMaterial),
		ReducerSourceSHA256:  reducerHash,
		RunnerSourceSHA256:   runnerHash,
		RuntimeSourceSHA256:  runtimeHash,
		CreatedAt:            createdAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func hashNonEmptyPaths(paths []string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("source paths must not be empty")
	}
	paths = append([]string(nil), paths...)
	sort.Strings(paths)
	hash := sha256.New()
	for _, path := range paths {
		value, err := hashNonEmptyPath(path)
		if err != nil {
			return "", err
		}
		_, _ = hash.Write([]byte(value))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func hashNonEmptyPath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("path must not be empty")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		files := make([]string, 0)
		if err := filepath.WalkDir(path, func(current string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				return nil
			}
			files = append(files, current)
			return nil
		}); err != nil {
			return "", err
		}
		sort.Strings(files)
		if len(files) == 0 {
			return "", errors.New("source directory has no non-test Go files")
		}
		hash := sha256.New()
		for _, current := range files {
			relative, err := filepath.Rel(path, current)
			if err != nil {
				return "", err
			}
			value, err := os.ReadFile(current)
			if err != nil {
				return "", err
			}
			_, _ = hash.Write([]byte(filepath.ToSlash(relative)))
			_, _ = hash.Write([]byte{0})
			_, _ = hash.Write(value)
			_, _ = hash.Write([]byte{0})
		}
		return hex.EncodeToString(hash.Sum(nil)), nil
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(value)) == 0 {
		return "", errors.New("file must not be empty")
	}
	return SHA256Bytes(value), nil
}

func requireDatasetID(kind, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s: dataset ID must not be empty", kind)
	}
	return nil
}

func decodeStrictJSONFile(path string, target any) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("path must not be empty")
	}
	value, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %q: %w", path, err)
	}
	if len(bytes.TrimSpace(value)) == 0 {
		return fmt.Errorf("read %q: file is empty", path)
	}
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode %q: %w", path, err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode %q: trailing JSON data", path)
		}
		return fmt.Errorf("decode %q: trailing JSON data: %w", path, err)
	}
	return nil
}

func canonicalizeJSON(value []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode canonical JSON: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, errors.New("canonical JSON contains trailing data")
		}
		return nil, fmt.Errorf("canonical JSON contains trailing data: %w", err)
	}
	return json.Marshal(decoded)
}
