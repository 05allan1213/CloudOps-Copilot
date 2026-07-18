package remediation

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type PatchResult struct {
	Content    []byte
	Diff       string
	BeforeHash string
	PatchHash  string
	Summary    string
	FileCount  int
}

// RenderPatch decodes YAML into an AST and changes exactly one typed field in
// one exact resource. It never accepts a path or a generic patch document.
func RenderPatch(source []byte, operation OperationType, parameters Parameters) (PatchResult, error) {
	if len(source) == 0 {
		return PatchResult{}, fmt.Errorf("%w: empty YAML", ErrInvalidArgument)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(source))
	var documents []*yaml.Node
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if err == io.EOF {
			break
		}
		if err != nil {
			return PatchResult{}, fmt.Errorf("%w: YAML decode", ErrInvalidArgument)
		}
		if len(document.Content) != 0 {
			documents = append(documents, &document)
		}
	}
	matched := 0
	var before, after, field string
	for _, document := range documents {
		root := document.Content[0]
		if !matchesResource(root, parameters.Target) {
			continue
		}
		matched++
		var err error
		switch operation {
		case OperationRollbackImage:
			before, after, field, err = mutateImage(root, parameters)
		case OperationSetReplicas:
			before, after, field, err = mutateReplicas(root, parameters)
		default:
			err = fmt.Errorf("%w: unsupported operation", ErrInvalidArgument)
		}
		if err != nil {
			return PatchResult{}, err
		}
	}
	if matched != 1 {
		return PatchResult{}, fmt.Errorf("%w: target resource matches=%d", ErrInvalidArgument, matched)
	}
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	for _, document := range documents {
		if err := encoder.Encode(document); err != nil {
			return PatchResult{}, fmt.Errorf("%w: YAML encode", ErrInvalidArgument)
		}
	}
	_ = encoder.Close()
	diff := deterministicDiff(parameters.Target, field, before, after)
	return PatchResult{Content: output.Bytes(), Diff: diff, BeforeHash: HashBytes(source), PatchHash: HashBytes([]byte(diff)), Summary: fmt.Sprintf("%s %s/%s: %s changed", parameters.Target.Kind, parameters.Target.Namespace, parameters.Target.Name, field), FileCount: 1}, nil
}

func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func matchesResource(root *yaml.Node, target TargetResource) bool {
	metadata := mapValue(root, "metadata")
	namespace := "default"
	if value := mapValue(metadata, "namespace"); value != nil {
		namespace = value.Value
	}
	return scalar(root, "apiVersion") == target.APIVersion && scalar(root, "kind") == target.Kind && scalar(metadata, "name") == target.Name && namespace == target.Namespace
}

func scalar(node *yaml.Node, key string) string {
	value := mapValue(node, key)
	if value == nil || value.Kind != yaml.ScalarNode {
		return ""
	}
	return value.Value
}

func mutateImage(root *yaml.Node, parameters Parameters) (string, string, string, error) {
	podSpec := mapValue(mapValue(mapValue(root, "spec"), "template"), "spec")
	containers := mapValue(podSpec, "containers")
	if containers == nil || containers.Kind != yaml.SequenceNode {
		return "", "", "", fmt.Errorf("%w: containers missing", ErrInvalidArgument)
	}
	matched := 0
	before := ""
	after := ""
	for _, container := range containers.Content {
		if scalar(container, "name") != parameters.Target.Container {
			continue
		}
		image := mapValue(container, "image")
		if image == nil || image.Kind != yaml.ScalarNode {
			return "", "", "", fmt.Errorf("%w: image missing", ErrInvalidArgument)
		}
		matched++
		before = image.Value
		after = immutableImage(before, parameters.ProposedValue.ImageDigest)
		if after == "" {
			return "", "", "", fmt.Errorf("%w: current image repository missing", ErrInvalidArgument)
		}
		image.Value = after
		image.Tag = "!!str"
	}
	if matched != 1 || before == after {
		return "", "", "", fmt.Errorf("%w: container matches=%d or value unchanged", ErrInvalidArgument, matched)
	}
	return before, after, "spec.template.spec.containers[name=" + parameters.Target.Container + "].image", nil
}

func immutableImage(current, digest string) string {
	current = strings.TrimSpace(current)
	if at := strings.Index(current, "@"); at > 0 {
		return current[:at] + "@" + digest
	}
	lastSlash := strings.LastIndex(current, "/")
	if colon := strings.LastIndex(current, ":"); colon > lastSlash {
		current = current[:colon]
	}
	if current == "" {
		return ""
	}
	return current + "@" + digest
}

func mutateReplicas(root *yaml.Node, parameters Parameters) (string, string, string, error) {
	if parameters.ProposedValue.Replicas == nil {
		return "", "", "", fmt.Errorf("%w: replicas missing", ErrInvalidArgument)
	}
	replicas := mapValue(mapValue(root, "spec"), "replicas")
	if replicas == nil || replicas.Kind != yaml.ScalarNode {
		return "", "", "", fmt.Errorf("%w: spec.replicas missing", ErrInvalidArgument)
	}
	before := replicas.Value
	after := strconv.Itoa(*parameters.ProposedValue.Replicas)
	if before == after {
		return "", "", "", fmt.Errorf("%w: value unchanged", ErrInvalidArgument)
	}
	replicas.Value, replicas.Tag = after, "!!int"
	return before, after, "spec.replicas", nil
}

func deterministicDiff(target TargetResource, field, before, after string) string {
	var b strings.Builder
	b.WriteString("--- approved/base\n+++ proposed/remediation\n@@ ")
	b.WriteString(target.APIVersion + "/" + target.Kind + "/" + target.Namespace + "/" + target.Name + " " + field + " @@\n")
	b.WriteString("-" + before + "\n+" + after + "\n")
	return b.String()
}
