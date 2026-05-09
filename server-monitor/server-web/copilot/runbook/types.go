package runbook

import (
	"errors"
	"time"
)

var ErrUnavailable = errors.New("runbook retriever unavailable")

type Document struct {
	ID               string
	Title            string
	File             string
	ApplicableAlerts []string
	Keywords         []string
	Metrics          []string
	Sections         []Section
	Body             string
	UpdatedAt        time.Time
}

type Section struct {
	Heading string
	Text    string
}

type SearchRequest struct {
	AlertName string   `json:"alert_name,omitempty"`
	Keywords  []string `json:"keywords,omitempty"`
	Metrics   []string `json:"metrics,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

type SearchResult struct {
	Title           string   `json:"title"`
	File            string   `json:"file"`
	Score           float64  `json:"score"`
	MatchedAlerts   []string `json:"matched_alerts,omitempty"`
	MatchedKeywords []string `json:"matched_keywords,omitempty"`
	MatchedMetrics  []string `json:"matched_metrics,omitempty"`
	Snippet         string   `json:"snippet"`
}

type LoadOptions struct {
	MaxFiles     int
	MaxFileBytes int64
}

type RetrieverOptions struct {
	DefaultLimit int
	MaxLimit     int
}
