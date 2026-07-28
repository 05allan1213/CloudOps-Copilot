package remediation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var (
	resourceNamePattern = regexp.MustCompile(`^[a-z0-9](?:[-a-z0-9.]*[a-z0-9])?$`)
	digestPattern       = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

var plannerOutputSchema = json.RawMessage(`{
  "$schema":"https://json-schema.org/draft/2020-12/schema",
  "type":"object",
  "additionalProperties":false,
  "required":["operation_type","target_resource","proposed_value","evidence_ids"],
  "properties":{
    "operation_type":{"enum":["rollback_image","set_replicas"]},
    "target_resource":{"type":"object","additionalProperties":false,"required":["api_version","kind","namespace","name"],"properties":{"api_version":{"type":"string"},"kind":{"enum":["Deployment","StatefulSet"]},"namespace":{"type":"string"},"name":{"type":"string"},"container":{"type":"string"}}},
    "proposed_value":{"type":"object","additionalProperties":false,"properties":{"image_digest":{"type":"string","pattern":"^sha256:[a-f0-9]{64}$"},"replicas":{"type":"integer"}}},
    "evidence_ids":{"type":"array","minItems":1,"maxItems":20,"uniqueItems":true,"items":{"type":"string","format":"uuid"}}
  }
}`)

// PlannerJSONSchema returns a copy of the fixed model-output schema.
func PlannerJSONSchema() json.RawMessage { return append(json.RawMessage(nil), plannerOutputSchema...) }

func DecodePlannerOutput(payload []byte) (PlannerOutput, error) {
	if len(payload) == 0 || len(payload) > MaxPlannerJSONBytes {
		return PlannerOutput{}, fmt.Errorf("%w: planner JSON size", ErrInvalidArgument)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var output PlannerOutput
	if err := decoder.Decode(&output); err != nil {
		return PlannerOutput{}, fmt.Errorf("%w: planner schema: %v", ErrInvalidArgument, err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return PlannerOutput{}, err
	}
	if err := output.Validate(); err != nil {
		return PlannerOutput{}, err
	}
	return output, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("%w: multiple JSON values", ErrInvalidArgument)
	}
	return nil
}

func (o PlannerOutput) Validate() error {
	if o.OperationType != OperationRollbackImage && o.OperationType != OperationSetReplicas {
		return fmt.Errorf("%w: unsupported operation", ErrInvalidArgument)
	}
	t := o.TargetResource
	if strings.TrimSpace(t.APIVersion) == "" || (t.Kind != "Deployment" && t.Kind != "StatefulSet") || !resourceNamePattern.MatchString(t.Namespace) || !resourceNamePattern.MatchString(t.Name) {
		return fmt.Errorf("%w: invalid target resource", ErrInvalidArgument)
	}
	if len(o.EvidenceIDs) == 0 || len(o.EvidenceIDs) > 20 {
		return fmt.Errorf("%w: evidence IDs", ErrInvalidArgument)
	}
	seen := make(map[string]struct{}, len(o.EvidenceIDs))
	for _, id := range o.EvidenceIDs {
		if len(id) != 36 {
			return fmt.Errorf("%w: invalid evidence ID", ErrInvalidArgument)
		}
		if _, exists := seen[id]; exists {
			return fmt.Errorf("%w: duplicate evidence ID", ErrInvalidArgument)
		}
		seen[id] = struct{}{}
	}
	switch o.OperationType {
	case OperationRollbackImage:
		if !resourceNamePattern.MatchString(t.Container) || !digestPattern.MatchString(strings.ToLower(o.ProposedValue.ImageDigest)) || o.ProposedValue.Replicas != nil {
			return fmt.Errorf("%w: rollback_image value", ErrInvalidArgument)
		}
	case OperationSetReplicas:
		if t.Container != "" || o.ProposedValue.Replicas == nil || o.ProposedValue.ImageDigest != "" {
			return fmt.Errorf("%w: set_replicas value", ErrInvalidArgument)
		}
	}
	return nil
}
