package tool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

func NormalizeArgs(schema ToolSchema, args json.RawMessage) (json.RawMessage, error) {
	values, err := decodeArgs(args)
	if err != nil {
		return nil, err
	}

	for _, param := range schema.Parameters {
		if strings.TrimSpace(param.Name) == "" {
			return nil, NewInvalidArgsError("schema", "parameter name is required")
		}

		value, exists := values[param.Name]
		if !exists || value == nil {
			if param.Required {
				return nil, NewInvalidArgsError(param.Name, "is required")
			}
			if param.Default != nil {
				values[param.Name] = param.Default
				value = param.Default
				exists = true
			}
		}
		if !exists || value == nil {
			continue
		}
		if err := validateParamValue(param, value); err != nil {
			return nil, err
		}
	}

	normalized, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal normalized arguments: %v", ErrInvalidArgs, err)
	}
	return normalized, nil
}

func ValidateArgs(schema ToolSchema, args json.RawMessage) error {
	_, err := NormalizeArgs(schema, args)
	return err
}

func decodeArgs(args json.RawMessage) (map[string]interface{}, error) {
	if len(bytes.TrimSpace(args)) == 0 {
		return map[string]interface{}{}, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(args))
	decoder.UseNumber()

	var decoded interface{}
	if err := decoder.Decode(&decoded); err != nil {
		return nil, NewInvalidArgsError("", "must be valid JSON")
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, NewInvalidArgsError("", "must contain one JSON value")
	}

	values, ok := decoded.(map[string]interface{})
	if !ok {
		return nil, NewInvalidArgsError("", "must be a JSON object")
	}
	return values, nil
}

func validateParamValue(param ParamSchema, value interface{}) error {
	switch param.Type {
	case ParamTypeString:
		text, ok := value.(string)
		if !ok {
			return NewInvalidArgsError(param.Name, "must be a string")
		}
		if err := validateEnum(param, text); err != nil {
			return err
		}
		if err := validateStringRange(param, text); err != nil {
			return err
		}
		if err := validatePattern(param, text); err != nil {
			return err
		}
	case ParamTypeNumber:
		number, ok := numericValue(value)
		if !ok {
			return NewInvalidArgsError(param.Name, "must be a number")
		}
		if err := validateEnum(param, formatNumber(number)); err != nil {
			return err
		}
		if err := validateNumberRange(param, number); err != nil {
			return err
		}
	case ParamTypeInteger:
		number, ok := integerValue(value)
		if !ok {
			return NewInvalidArgsError(param.Name, "must be an integer")
		}
		if err := validateEnum(param, strconv.FormatInt(number, 10)); err != nil {
			return err
		}
		if err := validateNumberRange(param, float64(number)); err != nil {
			return err
		}
	case ParamTypeBoolean:
		if _, ok := value.(bool); !ok {
			return NewInvalidArgsError(param.Name, "must be a boolean")
		}
	case ParamTypeArray:
		values, ok := value.([]interface{})
		if !ok {
			return NewInvalidArgsError(param.Name, "must be an array")
		}
		if err := validateLengthRange(param, len(values)); err != nil {
			return err
		}
	case ParamTypeObject:
		if _, ok := value.(map[string]interface{}); !ok {
			return NewInvalidArgsError(param.Name, "must be an object")
		}
	case "":
		return NewInvalidArgsError(param.Name, "type is required")
	default:
		return NewInvalidArgsError(param.Name, fmt.Sprintf("unsupported type %q", param.Type))
	}
	return nil
}

func validateEnum(param ParamSchema, value string) error {
	if len(param.Enum) == 0 {
		return nil
	}
	for _, allowed := range param.Enum {
		if value == allowed {
			return nil
		}
	}
	return NewInvalidArgsError(param.Name, "must be one of "+strings.Join(param.Enum, ", "))
}

func validatePattern(param ParamSchema, value string) error {
	if param.Pattern == "" {
		return nil
	}
	matched, err := regexp.MatchString(param.Pattern, value)
	if err != nil {
		return NewInvalidArgsError(param.Name, "has invalid pattern")
	}
	if !matched {
		return NewInvalidArgsError(param.Name, "does not match required pattern")
	}
	return nil
}

func validateStringRange(param ParamSchema, value string) error {
	return validateLengthRange(param, len([]rune(value)))
}

func validateLengthRange(param ParamSchema, length int) error {
	if param.Min != nil && float64(length) < *param.Min {
		return NewInvalidArgsError(param.Name, fmt.Sprintf("length must be >= %s", formatNumber(*param.Min)))
	}
	if param.Max != nil && float64(length) > *param.Max {
		return NewInvalidArgsError(param.Name, fmt.Sprintf("length must be <= %s", formatNumber(*param.Max)))
	}
	return nil
}

func validateNumberRange(param ParamSchema, value float64) error {
	if param.Min != nil && value < *param.Min {
		return NewInvalidArgsError(param.Name, "must be >= "+formatNumber(*param.Min))
	}
	if param.Max != nil && value > *param.Max {
		return NewInvalidArgsError(param.Name, "must be <= "+formatNumber(*param.Max))
	}
	return nil
}

func numericValue(value interface{}) (float64, bool) {
	switch typed := value.(type) {
	case json.Number:
		number, err := typed.Float64()
		return number, err == nil
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	default:
		reflected := reflect.ValueOf(value)
		switch reflected.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return float64(reflected.Int()), true
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return float64(reflected.Uint()), true
		default:
			return 0, false
		}
	}
}

func integerValue(value interface{}) (int64, bool) {
	switch typed := value.(type) {
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed, true
		}
		number, err := typed.Float64()
		if err != nil || math.Trunc(number) != number {
			return 0, false
		}
		return int64(number), true
	default:
		number, ok := numericValue(value)
		if !ok || math.Trunc(number) != number {
			return 0, false
		}
		return int64(number), true
	}
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
