package configutil

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func String(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return fallback
	}
	return value
}

func NonEmptyString(key, fallback string) string {
	value, exists := os.LookupEnv(key)
	if !exists || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func NonNegativeInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func PositiveInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func DurationSeconds(key string, fallback int) time.Duration {
	return time.Duration(PositiveInt(key, fallback)) * time.Second
}

func DurationMilliseconds(key string, fallback int) time.Duration {
	return time.Duration(PositiveInt(key, fallback)) * time.Millisecond
}

func FloatRange(key string, fallback, minValue, maxValue float64) float64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed < minValue || parsed > maxValue {
		return fallback
	}
	return parsed
}

func Bool(key string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func List(key string) []string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return nil
	}
	return splitList(value)
}

func ListWithFallback(key string, fallback []string) []string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return append([]string(nil), fallback...)
	}
	return splitList(strings.TrimSpace(value))
}

func splitList(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
