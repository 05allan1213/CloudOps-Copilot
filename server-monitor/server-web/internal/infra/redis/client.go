package rediscache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	pkgredis "server-monitor/pkg/redis"
)

var (
	rateLimitMemberPrefix = fmt.Sprintf("%d:%d", os.Getpid(), time.Now().UnixNano())
	rateLimitMemberSeq    atomic.Uint64
)

type Client struct {
	base *pkgredis.Client
}

type Options = pkgredis.Options

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

func (c *Client) Get(ctx context.Context, key string) ([]byte, bool) {
	if !c.Enabled() {
		return nil, false
	}

	value, err := c.base.Inner().Get(ctx, key).Bytes()
	if err != nil {
		if err != redis.Nil {
			zap.L().Error("redis get failed",
				zap.String("key", key),
				zap.Error(err),
			)
		}
		return nil, false
	}

	return value, true
}

func (c *Client) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}

	return c.base.Inner().Set(ctx, key, value, ttl).Err()
}

func (c *Client) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if !c.Enabled() {
		return false, errors.New("redis is not enabled")
	}

	return c.base.Inner().SetNX(ctx, key, value, ttl).Result()
}

func (c *Client) HSet(ctx context.Context, key, field string, value []byte) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}

	return c.base.Inner().HSet(ctx, key, field, value).Err()
}

func (c *Client) HDel(ctx context.Context, key, field string) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}

	return c.base.Inner().HDel(ctx, key, field).Err()
}

func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if !c.Enabled() {
		return nil, errors.New("redis is not enabled")
	}

	return c.base.Inner().HGetAll(ctx, key).Result()
}

func (c *Client) RPush(ctx context.Context, key string, values ...[]byte) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}
	if len(values) == 0 {
		return nil
	}

	args := make([]interface{}, 0, len(values))
	for _, value := range values {
		args = append(args, string(value))
	}
	return c.base.Inner().RPush(ctx, key, args...).Err()
}

func (c *Client) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	if !c.Enabled() {
		return nil, errors.New("redis is not enabled")
	}

	return c.base.Inner().LRange(ctx, key, start, stop).Result()
}

func (c *Client) LTrim(ctx context.Context, key string, start, stop int64) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}

	return c.base.Inner().LTrim(ctx, key, start, stop).Err()
}

func (c *Client) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}
	if ttl <= 0 {
		return errors.New("ttl must be positive")
	}

	return c.base.Inner().Expire(ctx, key, ttl).Err()
}

func (c *Client) Del(ctx context.Context, keys ...string) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}
	if len(keys) == 0 {
		return nil
	}

	return c.base.Inner().Del(ctx, keys...).Err()
}

func (c *Client) SAdd(ctx context.Context, key string, members ...string) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}
	if len(members) == 0 {
		return nil
	}

	args := make([]interface{}, 0, len(members))
	for _, member := range members {
		args = append(args, member)
	}
	return c.base.Inner().SAdd(ctx, key, args...).Err()
}

func (c *Client) SMembers(ctx context.Context, key string) ([]string, error) {
	if !c.Enabled() {
		return nil, errors.New("redis is not enabled")
	}

	return c.base.Inner().SMembers(ctx, key).Result()
}

func (c *Client) SRem(ctx context.Context, key string, members ...string) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}
	if len(members) == 0 {
		return nil
	}

	args := make([]interface{}, 0, len(members))
	for _, member := range members {
		args = append(args, member)
	}
	return c.base.Inner().SRem(ctx, key, args...).Err()
}

func (c *Client) AllowSlidingWindow(ctx context.Context, key string, limit int64, window time.Duration, now time.Time) (bool, int64, error) {
	if !c.Enabled() {
		return false, 0, errors.New("redis is not enabled")
	}
	if limit <= 0 {
		return false, 0, errors.New("rate limit must be positive")
	}
	if window <= 0 {
		return false, 0, errors.New("rate limit window must be positive")
	}

	nowUnixNano := now.UnixNano()
	windowStart := now.Add(-window).UnixNano()

	pipe := c.base.Inner().TxPipeline()
	pipe.ZRemRangeByScore(ctx, key, "0", "("+strconv.FormatInt(windowStart, 10))
	pipe.ZAdd(ctx, key, redis.Z{
		Score:  float64(nowUnixNano),
		Member: newRateLimitMember(nowUnixNano),
	})
	count := pipe.ZCard(ctx, key)
	pipe.Expire(ctx, key, window)

	if _, err := pipe.Exec(ctx); err != nil {
		return false, 0, err
	}

	used := count.Val()
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}

	return used <= limit, remaining, nil
}

func newRateLimitMember(nowUnixNano int64) string {
	return fmt.Sprintf("%s:%d:%d", rateLimitMemberPrefix, nowUnixNano, rateLimitMemberSeq.Add(1))
}

func (c *Client) XAddMaxLen(ctx context.Context, key string, maxLen int64, value []byte) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}
	if maxLen <= 0 {
		return errors.New("max stream length must be positive")
	}

	return c.base.Inner().XAdd(ctx, &redis.XAddArgs{
		Stream: key,
		MaxLen: maxLen,
		Approx: true,
		Values: map[string]interface{}{
			AlertEventPayload: string(value),
		},
	}).Err()
}

func (c *Client) AddAlertEventOnce(ctx context.Context, streamKey, dedupeKey string, maxLen int64, value, dedupeValue []byte, ttl time.Duration) (bool, error) {
	if !c.Enabled() {
		return false, errors.New("redis is not enabled")
	}
	if maxLen <= 0 {
		return false, errors.New("max stream length must be positive")
	}
	if ttl <= 0 {
		return false, errors.New("dedupe ttl must be positive")
	}

	result, err := c.base.Inner().Eval(ctx, `
if redis.call("EXISTS", KEYS[2]) == 1 then
	return 0
end
redis.call("XADD", KEYS[1], "MAXLEN", "~", ARGV[1], "*", ARGV[2], ARGV[3])
redis.call("SET", KEYS[2], ARGV[4], "PX", ARGV[5])
return 1
`, []string{streamKey, dedupeKey},
		strconv.FormatInt(maxLen, 10),
		AlertEventPayload,
		string(value),
		string(dedupeValue),
		strconv.FormatInt(ttl.Milliseconds(), 10),
	).Result()
	if err != nil {
		return false, err
	}

	stored, ok := result.(int64)
	if !ok {
		return false, fmt.Errorf("unexpected alert event script result %T", result)
	}

	return stored == 1, nil
}

func (c *Client) XRevRangeN(ctx context.Context, key string, count int64) ([]string, error) {
	if !c.Enabled() {
		return nil, errors.New("redis is not enabled")
	}
	if count <= 0 {
		return nil, errors.New("stream count must be positive")
	}

	messages, err := c.base.Inner().XRevRangeN(ctx, key, "+", "-", count).Result()
	if err != nil {
		return nil, err
	}

	values := make([]string, 0, len(messages))
	for _, message := range messages {
		raw, ok := message.Values[AlertEventPayload]
		if !ok {
			zap.L().Warn("skip alert event stream message without payload",
				zap.String("key", key),
				zap.String("id", message.ID),
			)
			continue
		}

		switch value := raw.(type) {
		case string:
			values = append(values, value)
		case []byte:
			values = append(values, string(value))
		default:
			zap.L().Warn("skip alert event stream message with invalid payload",
				zap.String("key", key),
				zap.String("id", message.ID),
			)
		}
	}

	return values, nil
}

func (c *Client) Publish(ctx context.Context, channel string, message []byte) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}

	return c.base.Inner().Publish(ctx, channel, message).Err()
}

func (c *Client) Subscribe(ctx context.Context, channel string) (<-chan string, error) {
	if !c.Enabled() {
		return nil, errors.New("redis is not enabled")
	}

	pubsub := c.base.Inner().Subscribe(ctx, channel)

	if err := pubsub.Ping(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("subscribe ping failed: %w", err), pubsub.Close())
	}

	source := pubsub.Channel()
	output := make(chan string, 32)

	go func() {
		defer close(output)
		defer func() {
			if err := pubsub.Close(); err != nil {
				zap.L().Warn("redis subscription cleanup failed")
			}
		}()

		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-source:
				if !ok {
					return
				}
				select {
				case <-ctx.Done():
					return
				case output <- message.Payload:
				}
			}
		}
	}()

	return output, nil
}
