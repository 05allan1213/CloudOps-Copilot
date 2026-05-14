package feedback

import (
	"regexp"
	"time"
)

const (
	RatingUseful    = "useful"
	RatingNotUseful = "not_useful"
)

var validRatings = map[string]bool{
	RatingUseful:    true,
	RatingNotUseful: true,
}

type FeedbackRequest struct {
	Rating  string `json:"rating" binding:"required"`
	Comment string `json:"comment,omitempty"`
}

type FeedbackResponse struct {
	ID          uint64    `json:"id"`
	DiagnosisID uint64    `json:"diagnosis_id"`
	Rating      string    `json:"rating"`
	Comment     string    `json:"comment,omitempty"`
	CreatedBy   uint64    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

var (
	ipPattern    = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
	keyPattern   = regexp.MustCompile(`(?i)(?:api[_-]?key|apikey|secret|token|password)\s*[:=]\s*\S+`)
	tokenPattern = regexp.MustCompile(`(?i)Bearer\s+\S+`)
)

func sanitizeComment(comment string) string {
	sanitized := comment
	sanitized = ipPattern.ReplaceAllString(sanitized, "[IP_REDACTED]")
	sanitized = keyPattern.ReplaceAllString(sanitized, "[KEY_REDACTED]")
	sanitized = tokenPattern.ReplaceAllString(sanitized, "[TOKEN_REDACTED]")
	return sanitized
}
