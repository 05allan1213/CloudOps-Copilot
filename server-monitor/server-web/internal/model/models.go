package model

import "server-web/internal/compatibility/legacyschema"

func AllModels() []interface{} {
	// Compatibility-only legacy tables remain in AutoMigrate so historical local
	// databases can open without a destructive migration. V2 durable truth lives
	// in explicit Goose migrations 00001-00006.
	return []interface{}{
		&User{},
		&legacyschema.HostGroup{},
		&legacyschema.HostGroupMember{},
		&legacyschema.AlertRule{},
		&legacyschema.NotificationChannel{},
		&AlertHistory{},
		&legacyschema.DiagnosisReport{},
		&legacyschema.DiagnosisFeedback{},
		&legacyschema.PendingAction{},
		&legacyschema.AuditLog{},
	}
}
