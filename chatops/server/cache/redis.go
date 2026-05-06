package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	Rdb *redis.Client
	Ctx = context.Background()
)

// Init 初始化 Redis 连接
func Init() {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	Rdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	})
}

// --- 对话历史缓存（List 结构）---

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// SaveMessage 追加一条消息到对话历史，最多保留 20 条
func SaveMessage(sessionID, role, content string) error {
	key := "chat:" + sessionID
	msg, _ := json.Marshal(Message{Role: role, Content: content})
	Rdb.RPush(Ctx, key, msg)
	Rdb.LTrim(Ctx, key, -20, -1)
	Rdb.Expire(Ctx, key, 30*time.Minute)
	return nil
}

// GetHistory 获取对话历史
func GetHistory(sessionID string) ([]Message, error) {
	key := "chat:" + sessionID
	vals, err := Rdb.LRange(Ctx, key, 0, -1).Result()
	if err != nil {
		return nil, err
	}
	msgs := make([]Message, 0, len(vals))
	for _, v := range vals {
		var m Message
		json.Unmarshal([]byte(v), &m)
		msgs = append(msgs, m)
	}
	return msgs, nil
}

// --- 查询结果缓存（String + TTL）---

// CacheQuery 缓存查询结果，TTL 30 秒
func CacheQuery(key string, value string) {
	Rdb.Set(Ctx, "query:"+key, value, 30*time.Second)
}

// GetCachedQuery 获取缓存的查询结果
func GetCachedQuery(key string) (string, bool) {
	val, err := Rdb.Get(Ctx, "query:"+key).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

// --- 接口限流（计数器，每分钟 60 次）---

// CheckRateLimit 检查是否超过限流，返回 true 表示被限流
func CheckRateLimit(clientIP string) bool {
	key := fmt.Sprintf("ratelimit:%s", clientIP)
	count, err := Rdb.Incr(Ctx, key).Result()
	if err != nil {
		return false
	}
	if count == 1 {
		Rdb.Expire(Ctx, key, time.Minute)
	}
	return count > 60
}
