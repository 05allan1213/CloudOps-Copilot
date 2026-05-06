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
)

var (
	rateLimitMemberPrefix = fmt.Sprintf("%d:%d", os.Getpid(), time.Now().UnixNano())
	rateLimitMemberSeq    atomic.Uint64
)

type Client struct {
	client  *redis.Client
	enabled bool
}

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

func NewClient(options Options) *Client {
	if options.Addr == "" {
		return &Client{}
	}

	return &Client{
		client: redis.NewClient(&redis.Options{
			Addr:            options.Addr,
			Password:        options.Password,
			DB:              options.DB,
			DialTimeout:     options.DialTimeout,
			ReadTimeout:     options.ReadTimeout,
			WriteTimeout:    options.WriteTimeout,
			ConnMaxLifetime: options.ConnMaxLifetime,
			ConnMaxIdleTime: options.ConnMaxIdleTime,
		}),
		enabled: true,
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.enabled
}

func (c *Client) Close() error {
	if !c.Enabled() {
		return nil
	}

	return c.client.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}

	return c.client.Ping(ctx).Err()
}

func (c *Client) Get(ctx context.Context, key string) ([]byte, bool) {
	if !c.Enabled() {
		return nil, false
	}

	value, err := c.client.Get(ctx, key).Bytes()
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

	return c.client.Set(ctx, key, value, ttl).Err()
}

func (c *Client) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if !c.Enabled() {
		return false, errors.New("redis is not enabled")
	}

	return c.client.SetNX(ctx, key, value, ttl).Result()
}

func (c *Client) HSet(ctx context.Context, key, field string, value []byte) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}

	return c.client.HSet(ctx, key, field, value).Err()
}

func (c *Client) HDel(ctx context.Context, key, field string) error {
	if !c.Enabled() {
		return errors.New("redis is not enabled")
	}

	return c.client.HDel(ctx, key, field).Err()
}

func (c *Client) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	if !c.Enabled() {
		return nil, errors.New("redis is not enabled")
	}

	return c.client.HGetAll(ctx, key).Result()
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

	pipe := c.client.TxPipeline()
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

	return c.client.XAdd(ctx, &redis.XAddArgs{
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

	result, err := c.client.Eval(ctx, `
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

	messages, err := c.client.XRevRangeN(ctx, key, "+", "-", count).Result()
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

	return c.client.Publish(ctx, channel, message).Err()
}

func (c *Client) Subscribe(ctx context.Context, channel string) (<-chan string, error) {
	if !c.Enabled() {
		return nil, errors.New("redis is not enabled")
	}

	pubsub := c.client.Subscribe(ctx, channel)

	if err := pubsub.Ping(ctx); err != nil {
		pubsub.Close()
		return nil, fmt.Errorf("subscribe ping failed: %w", err)
	}

	source := pubsub.Channel()
	output := make(chan string, 32)

	go func() {
		defer close(output)
		defer pubsub.Close()

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
