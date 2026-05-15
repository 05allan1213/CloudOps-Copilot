package redisstore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	pkgredis "server-monitor/pkg/redis"
)

type Client struct {
	base *pkgredis.Client
}

type Options = pkgredis.Options

var applyFiringEventScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 1 then
	return 0
end

local previous = redis.call("HGET", KEYS[2], ARGV[3])
local hsetResult = redis.pcall("HSET", KEYS[2], ARGV[3], ARGV[4])
if type(hsetResult) == "table" and hsetResult.err then
	return redis.error_reply("store active alert: " .. hsetResult.err)
end

local hincrResult = redis.pcall("HINCRBY", KEYS[3], ARGV[5], 1)
if type(hincrResult) == "table" and hincrResult.err then
	if previous then
		local restoreResult = redis.pcall("HSET", KEYS[2], ARGV[3], previous)
		if type(restoreResult) == "table" and restoreResult.err then
			return redis.error_reply("increment alert stats: " .. hincrResult.err .. "; rollback active alert: " .. restoreResult.err)
		end
	else
		local deleteResult = redis.pcall("HDEL", KEYS[2], ARGV[3])
		if type(deleteResult) == "table" and deleteResult.err then
			return redis.error_reply("increment alert stats: " .. hincrResult.err .. "; rollback active alert: " .. deleteResult.err)
		end
	end
	return redis.error_reply("increment alert stats: " .. hincrResult.err)
end

redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
return 1
`)

var applyResolvedEventScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[1]) == 1 then
	return 0
end

local hdelResult = redis.pcall("HDEL", KEYS[2], ARGV[3])
if type(hdelResult) == "table" and hdelResult.err then
	return redis.error_reply("delete active alert: " .. hdelResult.err)
end

redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
return 1
`)

func NewClient(options pkgredis.Options) *Client {
	return &Client{base: pkgredis.NewClient(options)}
}

func (c *Client) Enabled() bool {
	return c != nil && c.base != nil && c.base.Enabled()
}

func (c *Client) Close() error {
	if !c.Enabled() {
		return nil
	}
	return c.base.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}
	return c.base.Ping(ctx)
}

func (c *Client) ApplyFiringEvent(ctx context.Context, dedupKey string, ttl time.Duration, fingerprint string, payload []byte, statsField string) (bool, error) {
	if !c.Enabled() {
		return false, errors.New("redis is not enabled")
	}
	result, err := applyFiringEventScript.Run(ctx, c.base.Inner(),
		[]string{dedupKey, "alert:active", "alert:stats"},
		"1",
		ttl.Milliseconds(),
		fingerprint,
		string(payload),
		statsField,
	).Result()
	if err != nil {
		return false, err
	}
	return scriptStored(result)
}

func (c *Client) ApplyResolvedEvent(ctx context.Context, dedupKey string, ttl time.Duration, fingerprint string) (bool, error) {
	if !c.Enabled() {
		return false, errors.New("redis is not enabled")
	}
	result, err := applyResolvedEventScript.Run(ctx, c.base.Inner(),
		[]string{dedupKey, "alert:active"},
		"1",
		ttl.Milliseconds(),
		fingerprint,
	).Result()
	if err != nil {
		return false, err
	}
	return scriptStored(result)
}

func scriptStored(result interface{}) (bool, error) {
	switch value := result.(type) {
	case int64:
		return value == 1, nil
	case uint64:
		return value == 1, nil
	default:
		return false, fmt.Errorf("unexpected redis script result %T", result)
	}
}
