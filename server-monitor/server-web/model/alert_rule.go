package model

import "time"

type AlertRule struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"type:varchar(128);uniqueIndex;not null" json:"name"`
	Expr        string    `gorm:"type:text;not null" json:"expr"`
	Duration    string    `gorm:"type:varchar(32);not null;default:2m" json:"duration"`
	Severity    string    `gorm:"type:varchar(32);not null;default:warning" json:"severity"`
	Summary     string    `gorm:"type:varchar(512);not null;default:''" json:"summary"`
	Description string    `gorm:"type:text;not null" json:"description"`
	Enabled     bool      `gorm:"not null;index" json:"enabled"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
