package redis

import "time"

// Options configures the shared Redis client.
type Options struct {
	Addr            string
	Password        string
	DB              int
	DialTimeout     time.Duration
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}
