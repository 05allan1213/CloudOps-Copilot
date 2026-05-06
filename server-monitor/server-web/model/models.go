package model

func AllModels() []interface{} {
	return []interface{}{
		&User{},
		&HostGroup{},
		&HostGroupMember{},
		&AlertRule{},
		&NotificationChannel{},
		&AlertHistory{},
	}
}
