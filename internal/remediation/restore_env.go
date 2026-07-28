package remediation

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
	"go.yaml.in/yaml/v3"
)

type RestoreEnvPatch struct {
	Content       []byte
	Diff          string
	BeforeHash    string
	PostImageHash string
	Summary       string
	EnvNode       []byte
}

// RenderRestoreRequiredEnv copies one complete, non-secret EnvVar node from a
// verified baseline into the exact allowlisted Deployment/container.
func RenderRestoreRequiredEnv(current, baseline []byte, target TargetResource, envKey string) (RestoreEnvPatch, error) {
	if len(current) == 0 || len(baseline) == 0 || target.APIVersion != "apps/v1" || target.Kind != "Deployment" || target.Container == "" || !envKeyPattern.MatchString(envKey) {
		return RestoreEnvPatch{}, fmt.Errorf("%w: restore env input is invalid", ErrInvalidArgument)
	}
	currentDocs, err := decodeYAMLDocuments(current)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	baselineDocs, err := decodeYAMLDocuments(baseline)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	currentRoot, err := exactlyOneResource(currentDocs, target)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	baselineRoot, err := exactlyOneResource(baselineDocs, target)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	currentContainer, err := exactlyOneContainer(currentRoot, target.Container)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	baselineContainer, err := exactlyOneContainer(baselineRoot, target.Container)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	baselineEnv, count, err := findEnvNode(baselineContainer, envKey)
	if err != nil || count != 1 {
		return RestoreEnvPatch{}, fmt.Errorf("%w: baseline env matches=%d", ErrInvalidArgument, count)
	}
	if err := validateRestorableEnvNode(baselineEnv, envKey); err != nil {
		return RestoreEnvPatch{}, err
	}
	if _, count, err := findEnvNode(currentContainer, envKey); err != nil || count != 0 {
		return RestoreEnvPatch{}, fmt.Errorf("%w: current env already exists or is malformed", ErrInvalidArgument)
	}
	envSequence, err := ensureEnvSequence(currentContainer)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	envSequence.Content = append(envSequence.Content, cloneYAMLNode(baselineEnv))
	postImage, err := encodeYAMLDocuments(currentDocs)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	envBytes, err := encodeYAMLNode(baselineEnv)
	if err != nil {
		return RestoreEnvPatch{}, err
	}
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A: difflib.SplitLines(string(current)), B: difflib.SplitLines(string(postImage)),
		FromFile: "approved/base", ToFile: "proposed/remediation", Context: 3,
	})
	if err != nil || diff == "" {
		return RestoreEnvPatch{}, fmt.Errorf("%w: bounded unified diff generation failed", ErrInvalidArgument)
	}
	return RestoreEnvPatch{
		Content: postImage, Diff: diff, BeforeHash: HashBytes(current), PostImageHash: HashBytes(postImage),
		Summary: fmt.Sprintf("restore %s in Deployment %s/%s container %s", envKey, target.Namespace, target.Name, target.Container),
		EnvNode: envBytes,
	}, nil
}

func decodeYAMLDocuments(source []byte) ([]*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	documents := make([]*yaml.Node, 0, 1)
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: YAML decode", ErrInvalidArgument)
		}
		if len(document.Content) > 0 {
			documents = append(documents, &document)
		}
	}
	if len(documents) == 0 {
		return nil, fmt.Errorf("%w: YAML contains no resources", ErrInvalidArgument)
	}
	return documents, nil
}

func encodeYAMLDocuments(documents []*yaml.Node) ([]byte, error) {
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	for _, document := range documents {
		if err := encoder.Encode(document); err != nil {
			return nil, fmt.Errorf("%w: YAML encode", ErrInvalidArgument)
		}
	}
	if err := encoder.Close(); err != nil {
		return nil, fmt.Errorf("%w: YAML close", ErrInvalidArgument)
	}
	return output.Bytes(), nil
}

func encodeYAMLNode(node *yaml.Node) ([]byte, error) {
	document := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{cloneYAMLNode(node)}}
	return encodeYAMLDocuments([]*yaml.Node{document})
}

func exactlyOneResource(documents []*yaml.Node, target TargetResource) (*yaml.Node, error) {
	var matched *yaml.Node
	count := 0
	for _, document := range documents {
		if len(document.Content) == 0 || !matchesResource(document.Content[0], target) {
			continue
		}
		matched = document.Content[0]
		count++
	}
	if count != 1 {
		return nil, fmt.Errorf("%w: target resource matches=%d", ErrInvalidArgument, count)
	}
	return matched, nil
}

func exactlyOneContainer(root *yaml.Node, name string) (*yaml.Node, error) {
	podSpec := mapValue(mapValue(mapValue(root, "spec"), "template"), "spec")
	containers := mapValue(podSpec, "containers")
	if containers == nil || containers.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%w: Deployment containers missing", ErrInvalidArgument)
	}
	var matched *yaml.Node
	count := 0
	for _, container := range containers.Content {
		if scalar(container, "name") == name {
			matched = container
			count++
		}
	}
	if count != 1 {
		return nil, fmt.Errorf("%w: target container matches=%d", ErrInvalidArgument, count)
	}
	return matched, nil
}

func findEnvNode(container *yaml.Node, name string) (*yaml.Node, int, error) {
	env := mapValue(container, "env")
	if env == nil {
		return nil, 0, nil
	}
	if env.Kind != yaml.SequenceNode {
		return nil, 0, fmt.Errorf("%w: container env is not a sequence", ErrInvalidArgument)
	}
	var matched *yaml.Node
	count := 0
	for _, item := range env.Content {
		if item.Kind != yaml.MappingNode {
			return nil, 0, fmt.Errorf("%w: container env item is not an object", ErrInvalidArgument)
		}
		if scalar(item, "name") == name {
			matched = item
			count++
		}
	}
	return matched, count, nil
}

func ensureEnvSequence(container *yaml.Node) (*yaml.Node, error) {
	if container == nil || container.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("%w: target container is malformed", ErrInvalidArgument)
	}
	if env := mapValue(container, "env"); env != nil {
		if env.Kind != yaml.SequenceNode {
			return nil, fmt.Errorf("%w: target env is not a sequence", ErrInvalidArgument)
		}
		return env, nil
	}
	key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "env"}
	value := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	container.Content = append(container.Content, key, value)
	return value, nil
}

func validateRestorableEnvNode(node *yaml.Node, envKey string) error {
	if node == nil || node.Kind != yaml.MappingNode || scalar(node, "name") != envKey || containsAlias(node) {
		return fmt.Errorf("%w: baseline env node is malformed", ErrInvalidArgument)
	}
	allowed := map[string]struct{}{"name": {}, "value": {}, "valueFrom": {}}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index].Value
		if _, ok := allowed[key]; !ok || strings.EqualFold(key, "secretKeyRef") {
			return fmt.Errorf("%w: baseline env contains a forbidden field", ErrInvalidArgument)
		}
	}
	value, valueFrom := mapValue(node, "value"), mapValue(node, "valueFrom")
	if (value == nil) == (valueFrom == nil) {
		return fmt.Errorf("%w: baseline env must contain exactly one value source", ErrInvalidArgument)
	}
	if value != nil && (value.Kind != yaml.ScalarNode || len(value.Value) > 4096) {
		return fmt.Errorf("%w: baseline env value is invalid", ErrInvalidArgument)
	}
	if valueFrom != nil {
		if valueFrom.Kind != yaml.MappingNode || containsMappingKey(valueFrom, "secretKeyRef") {
			return fmt.Errorf("%w: Secret-backed env restore is forbidden", ErrInvalidArgument)
		}
		if len(valueFrom.Content) != 2 {
			return fmt.Errorf("%w: env valueFrom must select exactly one source", ErrInvalidArgument)
		}
		for index := 0; index+1 < len(valueFrom.Content); index += 2 {
			key := valueFrom.Content[index].Value
			if key != "configMapKeyRef" && key != "fieldRef" && key != "resourceFieldRef" {
				return fmt.Errorf("%w: unsupported env valueFrom source", ErrInvalidArgument)
			}
		}
	}
	return nil
}

func containsMappingKey(node *yaml.Node, target string) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			if node.Content[index].Value == target || containsMappingKey(node.Content[index+1], target) {
				return true
			}
		}
		return false
	}
	for _, child := range node.Content {
		if containsMappingKey(child, target) {
			return true
		}
	}
	return false
}

func containsAlias(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.AliasNode || node.Alias != nil {
		return true
	}
	for _, child := range node.Content {
		if containsAlias(child) {
			return true
		}
	}
	return false
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}
	clone := *node
	clone.Alias = nil
	clone.Content = make([]*yaml.Node, len(node.Content))
	for index, child := range node.Content {
		clone.Content[index] = cloneYAMLNode(child)
	}
	return &clone
}
