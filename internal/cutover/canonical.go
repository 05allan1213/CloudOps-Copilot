package cutover

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
)

const canonicalHashVersion uint16 = 1

// canonicalJSON normalizes a JSON value without losing integer precision.
// Object keys are sorted and every scalar uses the JSON lexical form consumed
// by the decoder. It is used only for hashes and bounded archive snapshots;
// raw payloads are never emitted by the audit exporter.
func canonicalJSON(value []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errors.New("canonical JSON contains multiple values")
	}
	var output bytes.Buffer
	if err := appendCanonicalJSON(&output, decoded); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendCanonicalJSON(output *bytes.Buffer, value any) error {
	switch typed := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		output.WriteString(strconv.FormatBool(typed))
	case string:
		encoded, _ := json.Marshal(typed)
		output.Write(encoded)
	case json.Number:
		if _, err := typed.Int64(); err != nil {
			if _, floatErr := strconv.ParseFloat(typed.String(), 64); floatErr != nil {
				return fmt.Errorf("invalid JSON number %q", typed)
			}
		}
		output.WriteString(typed.String())
	case []any:
		output.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonicalJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			encodedKey, _ := json.Marshal(key)
			output.Write(encodedKey)
			output.WriteByte(':')
			if err := appendCanonicalJSON(output, typed[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON type %T", value)
	}
	return nil
}

func canonicalHashJSON(value []byte) (string, []byte, error) {
	canonical, err := canonicalJSON(value)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), canonical, nil
}

// canonicalHashFields uses a versioned, length-prefixed binary encoding. It
// avoids the ambiguity of SQL CONCAT/CONCAT_WS and is stable across retries.
func canonicalHashFields(fields ...string) string {
	var encoded bytes.Buffer
	encoded.WriteString("cloudops-cutover-canonical\x00")
	encoded.WriteString(strconv.FormatUint(uint64(canonicalHashVersion), 10))
	encoded.WriteByte(0)
	for _, field := range fields {
		encoded.WriteString(strconv.Itoa(len(field)))
		encoded.WriteByte(':')
		encoded.WriteString(field)
		encoded.WriteByte(0)
	}
	sum := sha256.Sum256(encoded.Bytes())
	return hex.EncodeToString(sum[:])
}

func canonicalHashSet(values []string) string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	return canonicalHashFields(copyValues...)
}

func canonicalComponent(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "<canonical-encode-error>"
	}
	canonical, err := canonicalJSON(encoded)
	if err != nil {
		return "<canonical-json-error>"
	}
	return string(canonical)
}

func releaseIdentityHash(sourceSHA, imageDigest string, sourceSchema, targetSchema uint64) string {
	return canonicalHashFields(
		"release-identity/v1",
		sourceSHA,
		imageDigest,
		strconv.FormatUint(sourceSchema, 10),
		strconv.FormatUint(targetSchema, 10),
	)
}
