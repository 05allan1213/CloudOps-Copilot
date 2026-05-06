package model

import "time"

type AlertHistory struct {
	ID          uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Fingerprint string     `gorm:"type:varchar(64);index:idx_alert_history_fingerprint_fired_at;not null;default:''" json:"fingerprint"`
	AlertName   string     `gorm:"type:varchar(128);index;not null;default:''" json:"alert_name"`
	Instance    string     `gorm:"type:varchar(256);index;not null;default:''" json:"instance"`
	Severity    string     `gorm:"type:varchar(32);index;not null;default:warning" json:"severity"`
	Status      string     `gorm:"type:varchar(32);index;not null;default:firing" json:"status"`
	Summary     string     `gorm:"type:varchar(512);not null;default:''" json:"summary"`
	LabelsJSON  string     `gorm:"column:labels_json;type:text;not null" json:"labels_json"`
	FiredAt     time.Time  `gorm:"not null;index;index:idx_alert_history_fingerprint_fired_at" json:"fired_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	CreatedAt   time.Time  `gorm:"autoCreateTime" json:"created_at"`
}
