package model

func AllModels() []interface{} {
	// Compatibility-only legacy tables remain in AutoMigrate so existing local
	// databases can still open without a destructive migration. Step 2 removes
	// their public routes and writers; V2 durable truth lives in Goose 00001-00006.
	return []interface{}{
		&User{},
		&HostGroup{},
		&HostGroupMember{},
		&AlertRule{},
		&NotificationChannel{},
		&AlertHistory{},
		&DiagnosisReport{},
		&DiagnosisFeedback{},
		&PendingAction{},
		&AuditLog{},
	}
}
