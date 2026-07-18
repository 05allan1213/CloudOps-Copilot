package model

import "github.com/05allan1213/CloudOps-Copilot/internal/compatibility/legacyschema"

func AllModels() []interface{} {
	// This inventory is retained for test-only V2 schema fixtures. Runtime schema
	// ownership lives exclusively in explicit Goose migrations.
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
