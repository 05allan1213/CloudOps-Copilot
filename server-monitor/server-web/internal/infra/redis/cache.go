package rediscache

const (
	ActiveAlertsKey     = "alert:active"
	AlertEventsKey      = "alert:events"
	AlertEventDedupeKey = "alert:event:dedupe"
	AlertEventPayload   = "payload"
	AlertChannel        = "alert:channel"
	AlertEventsMax      = 200
	RateLimitKeyPrefix  = "ratelimit"
)
