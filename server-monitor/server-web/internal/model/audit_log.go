package model

import "time"

const (
	AuditResultSuccess = "success"
	AuditResultFailure = "failure"
	AuditResultDenied  = "denied"
	AuditResultTimeout = "timeout"
)

type AuditLog struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Actor        string    `gorm:"type:varchar(128);index;not null" json:"actor"`
	ActorRole    string    `gorm:"type:varchar(32);index;not null" json:"actor_role"`
	Action       string    `gorm:"type:varchar(64);index;not null" json:"action"`
	ResourceType string    `gorm:"type:varchar(64);index;not null" json:"resource_type"`
	ResourceID   string    `gorm:"type:varchar(128);index;not null" json:"resource_id"`
	RequestJSON  string    `gorm:"column:request_json;type:longtext;not null" json:"request_json"`
	Result       string    `gorm:"type:varchar(32);index;not null" json:"result"`
	ErrorMessage string    `gorm:"type:text;not null" json:"error_message"`
	TraceID      string    `gorm:"type:varchar(64);index;not null" json:"trace_id"`
	CreatedAt    time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}
