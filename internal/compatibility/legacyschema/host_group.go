// Package legacyschema contains compatibility-only GORM shapes for opening
// historical local databases. V2 code must not use these types for new writes.
package legacyschema

import "time"

type HostGroup struct {
	ID          uint64            `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string            `gorm:"type:varchar(128);uniqueIndex;not null" json:"name"`
	Description string            `gorm:"type:varchar(512);not null;default:''" json:"description"`
	Members     []HostGroupMember `gorm:"foreignKey:GroupID;constraint:OnDelete:CASCADE" json:"members,omitempty"`
	CreatedAt   time.Time         `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time         `gorm:"autoUpdateTime" json:"updated_at"`
}

type HostGroupMember struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	GroupID   uint64    `gorm:"uniqueIndex:uk_group_instance;not null" json:"group_id"`
	Instance  string    `gorm:"type:varchar(256);uniqueIndex:uk_group_instance;index;not null" json:"instance"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}
