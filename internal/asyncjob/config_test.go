package asyncjob

import (
	"context"
	"testing"
	"time"
)

func TestDefaultPoolConfigMatchesFrozenContract(t *testing.T) {
	t.Parallel()
	pools := DefaultPoolConfigs()
	want := map[Queue]PoolConfig{
		QueueInvestigate: {MaxInFlight: 2, HandlerDeadline: 45 * time.Second, LeaseDuration: 90 * time.Second, HeartbeatPeriod: 20 * time.Second, ExternalDeadline: 40 * time.Second},
		QueueDeliver:     {MaxInFlight: 1, HandlerDeadline: 30 * time.Second, LeaseDuration: 60 * time.Second, HeartbeatPeriod: 15 * time.Second, ExternalDeadline: 25 * time.Second},
		QueueObserve:     {MaxInFlight: 2, HandlerDeadline: 20 * time.Second, LeaseDuration: 45 * time.Second, HeartbeatPeriod: 10 * time.Second, ExternalDeadline: 15 * time.Second},
		QueueVerify:      {MaxInFlight: 2, HandlerDeadline: 20 * time.Second, LeaseDuration: 45 * time.Second, HeartbeatPeriod: 10 * time.Second, ExternalDeadline: 15 * time.Second},
	}
	for queue, expected := range want {
		if got := pools[queue]; got != expected {
			t.Fatalf("%s config=%+v, want %+v", queue, got, expected)
		}
	}
}

func TestPoolConfigTimingRelationships(t *testing.T) {
	t.Parallel()
	valid := PoolConfig{MaxInFlight: 1, ExternalDeadline: time.Second, HandlerDeadline: 2 * time.Second, HeartbeatPeriod: time.Second, LeaseDuration: 3 * time.Second}
	if err := valid.Validate(QueueDeliver); err != nil {
		t.Fatal(err)
	}
	cases := []PoolConfig{
		{MaxInFlight: 1, ExternalDeadline: 2 * time.Second, HandlerDeadline: 2 * time.Second, HeartbeatPeriod: time.Second, LeaseDuration: 4 * time.Second},
		{MaxInFlight: 1, ExternalDeadline: time.Second, HandlerDeadline: 3 * time.Second, HeartbeatPeriod: time.Second, LeaseDuration: 3 * time.Second},
		{MaxInFlight: 1, ExternalDeadline: time.Second, HandlerDeadline: 2 * time.Second, HeartbeatPeriod: time.Second + time.Nanosecond, LeaseDuration: 3 * time.Second},
	}
	for _, candidate := range cases {
		if err := candidate.Validate(QueueDeliver); err == nil {
			t.Fatalf("invalid timing accepted: %+v", candidate)
		}
	}
}

func TestRunnerConfigRejectsMissingAndExtraHandlers(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	config := RunnerConfig{Owner: "worker", Store: store, Handlers: successfulHandlers()}
	config.applyDefaults()
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	delete(config.Handlers, TaskVerificationAdvance)
	if err := config.Validate(); err == nil {
		t.Fatal("missing frozen handler was accepted")
	}
	config.Handlers[TaskVerificationAdvance] = HandlerFunc(func(context.Context, Execution) Result { return Succeeded(nil) })
	config.Handlers["other"] = HandlerFunc(func(context.Context, Execution) Result { return Succeeded(nil) })
	if err := config.Validate(); err == nil {
		t.Fatal("extra handler was accepted")
	}
}
