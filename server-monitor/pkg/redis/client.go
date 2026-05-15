// Package redis provides shared Redis client primitives.
package redis

import (
	"context"
	"errors"

	goredis "github.com/redis/go-redis/v9"
)

// Client wraps the base go-redis client used by services.
type Client struct {
	client  *goredis.Client
	options Options
	enabled bool
}

// NewClient creates a Redis client. Empty Addr disables Redis.
func NewClient(options Options) *Client {
	if options.Addr == "" {
		return &Client{options: options}
	}

	client := goredis.NewClient(&goredis.Options{
		Addr:            options.Addr,
		Password:        options.Password,
		DB:              options.DB,
		DialTimeout:     options.DialTimeout,
		ReadTimeout:     options.ReadTimeout,
		WriteTimeout:    options.WriteTimeout,
		ConnMaxLifetime: options.ConnMaxLifetime,
		ConnMaxIdleTime: options.ConnMaxIdleTime,
	})

	return &Client{
		client:  client,
		options: options,
		enabled: true,
	}
}

// Enabled reports whether Redis is configured.
func (c *Client) Enabled() bool {
	return c != nil && c.enabled
}

// Close closes the underlying Redis client.
func (c *Client) Close() error {
	if !c.Enabled() {
		return nil
	}
	return c.client.Close()
}

// Ping checks Redis connectivity.
func (c *Client) Ping(ctx context.Context) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}
	return c.client.Ping(ctx).Err()
}

// Options returns the configuration used to create the client.
func (c *Client) Options() Options {
	if c == nil {
		return Options{}
	}
	return c.options
}

// Inner returns the underlying go-redis client for service-specific operations.
func (c *Client) Inner() *goredis.Client {
	if !c.Enabled() {
		return nil
	}
	return c.client
}
