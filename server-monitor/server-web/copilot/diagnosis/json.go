package diagnosis

import (
	"encoding/json"
	"fmt"
)

func marshalJSON(value interface{}) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if !json.Valid(data) {
		return "", fmt.Errorf("invalid json")
	}
	return string(data), nil
}

func rawJSON(value string) json.RawMessage {
	if value == "" || !json.Valid([]byte(value)) {
		return nil
	}
	return json.RawMessage(value)
}
