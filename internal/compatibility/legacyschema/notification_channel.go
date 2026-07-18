// Package legacyschema contains compatibility-only GORM shapes for opening
// historical local databases. V2 code must not use these types for new writes.
package legacyschema

import "time"

type NotificationChannel struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `gorm:"type:varchar(128);uniqueIndex;not null" json:"name"`
	Type      string    `gorm:"type:varchar(32);not null;default:webhook;index" json:"type"`
	URL       string    `gorm:"type:varchar(512);not null;default:''" json:"url"`
	Enabled   bool      `gorm:"not null" json:"enabled"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}
