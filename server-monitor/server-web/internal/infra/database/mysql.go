package database

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type MySQLConfig struct {
	Host        string
	Port        string
	User        string
	Password    string
	Database    string
	PingTimeout time.Duration
}

type MySQL struct {
	db          *gorm.DB
	sqlDB       *sql.DB
	pingTimeout time.Duration
}

func (c MySQLConfig) Enabled() bool {
	return c.Host != "" || c.User != "" || c.Database != ""
}

func (c MySQLConfig) DSN() string {
	port := c.Port
	if port == "" {
		port = "3306"
	}
	driverConfig := drivermysql.Config{
		User:      c.User,
		Passwd:    c.Password,
		Net:       "tcp",
		Addr:      net.JoinHostPort(c.Host, port),
		DBName:    c.Database,
		ParseTime: true,
	}
	return driverConfig.FormatDSN()
}

func OpenMySQL(ctx context.Context, cfg MySQLConfig) (*MySQL, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	if cfg.Host == "" {
		return nil, errors.New("mysql host is required")
	}
	if cfg.User == "" {
		return nil, errors.New("mysql user is required")
	}
	if cfg.Database == "" {
		return nil, errors.New("mysql database is required")
	}

	db, err := gorm.Open(gormmysql.Open(cfg.DSN()), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	client := &MySQL{
		db:          db,
		sqlDB:       sqlDB,
		pingTimeout: cfg.PingTimeout,
	}
	if err := client.Ping(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return client, nil
}

func (m *MySQL) Enabled() bool {
	return m != nil && m.sqlDB != nil
}

func (m *MySQL) DB() *gorm.DB {
	if m == nil {
		return nil
	}
	return m.db
}

func (m *MySQL) Ping(ctx context.Context) error {
	if !m.Enabled() {
		return nil
	}

	pingCtx := ctx
	cancel := func() {}
	if m.pingTimeout > 0 {
		pingCtx, cancel = context.WithTimeout(ctx, m.pingTimeout)
	}
	defer cancel()

	return m.sqlDB.PingContext(pingCtx)
}

func (m *MySQL) Close() error {
	if !m.Enabled() {
		return nil
	}
	return m.sqlDB.Close()
}
