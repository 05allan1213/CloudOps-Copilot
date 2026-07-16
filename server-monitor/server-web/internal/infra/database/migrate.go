package database

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"

	"gorm.io/gorm"

	"server-web/internal/model"
)

const autoMigrateLockTimeoutSeconds = 60

func Migrate(db *gorm.DB) error {
	if db == nil {
		return errors.New("gorm db is required")
	}

	// MySQL advisory locks are connection-scoped, so pin acquisition,
	// migration, and release to the same database connection.
	return db.Connection(func(tx *gorm.DB) (retErr error) {
		var databaseName sql.NullString
		if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT DATABASE()").Row().Scan(&databaseName); err != nil {
			return fmt.Errorf("read migration database name: %w", err)
		}
		if !databaseName.Valid || databaseName.String == "" {
			return errors.New("migration database name is empty")
		}

		lockName := fmt.Sprintf("%x", sha256.Sum256([]byte("cloudops-copilot:auto-migrate:"+databaseName.String)))
		var acquired sql.NullInt64
		if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT GET_LOCK(?, ?)", lockName, autoMigrateLockTimeoutSeconds).Row().Scan(&acquired); err != nil {
			return fmt.Errorf("acquire migration lock: %w", err)
		}
		if !acquired.Valid || acquired.Int64 != 1 {
			return fmt.Errorf("acquire migration lock %q timed out", lockName)
		}

		defer func() {
			var released sql.NullInt64
			if err := tx.Session(&gorm.Session{NewDB: true}).Raw("SELECT RELEASE_LOCK(?)", lockName).Row().Scan(&released); err != nil {
				retErr = errors.Join(retErr, fmt.Errorf("release migration lock: %w", err))
				return
			}
			if !released.Valid || released.Int64 != 1 {
				retErr = errors.Join(retErr, fmt.Errorf("release migration lock %q failed", lockName))
			}
		}()

		return tx.Session(&gorm.Session{NewDB: true}).AutoMigrate(model.AllModels()...)
	})
}
