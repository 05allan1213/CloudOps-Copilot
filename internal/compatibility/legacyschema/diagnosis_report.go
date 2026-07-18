// Package legacyschema contains compatibility-only GORM shapes for opening
// historical local databases. V2 code must not use these types for new writes.
package legacyschema

import "time"

type DiagnosisReport struct {
	ID                     uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	AlertHistoryID         uint64    `gorm:"index;not null;default:0" json:"alert_history_id"`
	Fingerprint            string    `gorm:"type:varchar(128);index;not null;default:''" json:"fingerprint"`
	AlertName              string    `gorm:"type:varchar(128);index;not null;default:''" json:"alert_name"`
	TargetKind             string    `gorm:"type:varchar(64);not null;default:'host'" json:"target_kind"`
	TargetName             string    `gorm:"type:varchar(256);not null;default:''" json:"target_name"`
	Namespace              string    `gorm:"type:varchar(128);not null;default:''" json:"namespace"`
	Severity               string    `gorm:"type:varchar(32);index;not null;default:'warning'" json:"severity"`
	Status                 string    `gorm:"type:varchar(32);index;not null;default:'pending'" json:"status"`
	Summary                string    `gorm:"type:text;not null" json:"summary"`
	RootCause              string    `gorm:"type:text;not null" json:"root_cause"`
	EvidenceJSON           string    `gorm:"column:evidence_json;type:longtext;not null" json:"evidence_json"`
	RunbooksJSON           string    `gorm:"column:runbooks_json;type:longtext;not null" json:"runbooks_json"`
	RecommendedActionsJSON string    `gorm:"column:recommended_actions_json;type:longtext;not null" json:"recommended_actions_json"`
	RuleAnalysisJSON       string    `gorm:"column:rule_analysis_json;type:longtext;not null" json:"rule_analysis_json"`
	Confidence             float64   `gorm:"not null;default:0" json:"confidence"`
	LLMPromptHash          string    `gorm:"type:varchar(64);not null;default:''" json:"llm_prompt_hash"`
	LLMModel               string    `gorm:"type:varchar(128);not null;default:''" json:"llm_model"`
	TriggerType            string    `gorm:"type:varchar(32);not null;default:'manual'" json:"trigger_type"`
	CreatedBy              uint64    `gorm:"index;not null;default:0" json:"created_by"`
	CreatedAt              time.Time `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt              time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
