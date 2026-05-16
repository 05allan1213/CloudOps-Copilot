package tool

import (
	"strings"

	"server-web/internal/copilot/llm"
)

var paramTypeMap = map[ParamType]string{
	ParamTypeString:  "string",
	ParamTypeNumber:  "number",
	ParamTypeInteger: "integer",
	ParamTypeBoolean: "boolean",
	ParamTypeArray:   "array",
	ParamTypeObject:  "object",
}

func ConvertToOpenAITools(schemas []ToolSchema) []llm.ToolDefinition {
	var result []llm.ToolDefinition
	for _, schema := range schemas {
		if !schema.ReadOnly {
			continue
		}
		if schema.RiskLevel == RiskLevelHigh {
			continue
		}
		td, ok := convertToolSchema(schema)
		if !ok {
			continue
		}
		result = append(result, td)
	}
	return result
}

func convertToolSchema(schema ToolSchema) (llm.ToolDefinition, bool) {
	properties := make(map[string]interface{}, len(schema.Parameters))
	var required []string

	for _, param := range schema.Parameters {
		prop := convertParamSchema(param)
		if prop == nil {
			continue
		}
		properties[param.Name] = prop
		if param.Required {
			required = append(required, param.Name)
		}
	}

	params := map[string]interface{}{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		params["required"] = required
	} else {
		params["required"] = []string{}
	}

	return llm.ToolDefinition{
		Type: "function",
		Function: llm.ToolFunction{
			Name:        registryNameToOpenAIName(schema.Name),
			Description: schema.Description,
			Parameters:  params,
		},
	}, true
}

func convertParamSchema(param ParamSchema) map[string]interface{} {
	typeStr, ok := paramTypeMap[param.Type]
	if !ok {
		return nil
	}
	prop := map[string]interface{}{
		"type": typeStr,
	}
	if param.Description != "" {
		prop["description"] = param.Description
	}
	if len(param.Enum) > 0 {
		prop["enum"] = param.Enum
	}
	if param.Min != nil {
		prop["minimum"] = *param.Min
	}
	if param.Max != nil {
		prop["maximum"] = *param.Max
	}
	if param.Pattern != "" {
		prop["pattern"] = param.Pattern
	}
	return prop
}

func registryNameToOpenAIName(name string) string {
	return strings.ReplaceAll(name, ".", "_")
}
