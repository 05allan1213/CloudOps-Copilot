// Package legacyschema contains compatibility-only GORM shapes for opening
// historical local databases. V2 code must not use these types for new writes.
package legacyschema

import "time"

const (
	ActionStatusPending   = "pending"
	ActionStatusApproved  = "approved"
	ActionStatusRejected  = "rejected"
	ActionStatusExecuting = "executing"
	ActionStatusExecuted  = "executed"
	ActionStatusFailed    = "failed"
	ActionStatusCancelled = "cancelled"

	ActionRiskLow    = "low"
	ActionRiskMedium = "medium"
	ActionRiskHigh   = "high"
)

type PendingAction struct {
	ID                uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	DiagnosisReportID uint64     `gorm:"index;not null;default:0" json:"diagnosis_report_id"`
	ActionType        string     `gorm:"type:varchar(64);index;not null" json:"action_type"`
	TargetKind        string     `gorm:"type:varchar(32);not null" json:"target_kind"`
	TargetName        string     `gorm:"type:varchar(256);index;not null" json:"target_name"`
	Namespace         string     `gorm:"type:varchar(128);index;not null" json:"namespace"`
	ParamsJSON        string     `gorm:"column:params_json;type:longtext;not null" json:"params_json"`
	DedupeKey         string     `gorm:"type:varchar(128);uniqueIndex;not null" json:"dedupe_key"`
	RiskLevel         string     `gorm:"type:varchar(16);index;not null" json:"risk_level"`
	Status            string     `gorm:"type:varchar(32);index;not null;default:'pending'" json:"status"`
	RequestedBy       string     `gorm:"type:varchar(32);not null;default:'ai-copilot'" json:"requested_by"`
	ApprovedBy        uint64     `gorm:"not null;default:0" json:"approved_by"`
	ExecutedBy        uint64     `gorm:"not null;default:0" json:"executed_by"`
	ResultJSON        string     `gorm:"column:result_json;type:longtext;not null" json:"result_json"`
	ErrorMessage      string     `gorm:"type:text;not null" json:"error_message"`
	CreatedAt         time.Time  `gorm:"autoCreateTime;index" json:"created_at"`
	ApprovedAt        *time.Time `json:"approved_at,omitempty"`
	ExecutedAt        *time.Time `json:"executed_at,omitempty"`
	UpdatedAt         time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}
