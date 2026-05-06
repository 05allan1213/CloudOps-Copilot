package database

import (
	"errors"

	"gorm.io/gorm"

	"server-web/model"
)

func Migrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("gorm db is required")
	}
	return db.AutoMigrate(model.AllModels()...)
}
