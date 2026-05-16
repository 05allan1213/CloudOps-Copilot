package model

import "time"

type DiagnosisFeedback struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	DiagnosisID uint64    `gorm:"index;not null;uniqueIndex:idx_diagnosis_user" json:"diagnosis_id"`
	Rating      string    `gorm:"type:varchar(16);not null" json:"rating"`
	Comment     string    `gorm:"type:varchar(512);not null;default:''" json:"comment"`
	CreatedBy   uint64    `gorm:"index;not null;default:0;uniqueIndex:idx_diagnosis_user" json:"created_by"`
	CreatedAt   time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (DiagnosisFeedback) TableName() string {
	return "diagnosis_feedback"
}
