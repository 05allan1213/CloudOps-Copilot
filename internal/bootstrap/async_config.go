package bootstrap

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// AsyncWorkerConfig is the typed, bounded configuration for the task runtime.
type AsyncWorkerConfig struct {
	WorkerID string

	InvestigateMaxInFlight int
	DeliverMaxInFlight     int
	ObserveMaxInFlight     int
	VerifyMaxInFlight      int

	InvestigateHandlerDeadline time.Duration
	DeliverHandlerDeadline     time.Duration
	ObserveHandlerDeadline     time.Duration
	VerifyHandlerDeadline      time.Duration

	InvestigateExternalDeadline time.Duration
	DeliverExternalDeadline     time.Duration
	ObserveExternalDeadline     time.Duration
	VerifyExternalDeadline      time.Duration

	InvestigateLease time.Duration
	DeliverLease     time.Duration
	ObserveLease     time.Duration
	VerifyLease      time.Duration

	InvestigateHeartbeat time.Duration
	DeliverHeartbeat     time.Duration
	ObserveHeartbeat     time.Duration
	VerifyHeartbeat      time.Duration

	DrainTimeout time.Duration
	ExitDeadline time.Duration
}

// DefaultAsyncWorkerConfig returns the frozen pool sizes and timing contract.
func DefaultAsyncWorkerConfig() AsyncWorkerConfig {
	return AsyncWorkerConfig{
		WorkerID:                    "cloudops-worker",
		InvestigateMaxInFlight:      2,
		DeliverMaxInFlight:          1,
		ObserveMaxInFlight:          2,
		VerifyMaxInFlight:           2,
		InvestigateHandlerDeadline:  45 * time.Second,
		DeliverHandlerDeadline:      30 * time.Second,
		ObserveHandlerDeadline:      20 * time.Second,
		VerifyHandlerDeadline:       20 * time.Second,
		InvestigateExternalDeadline: 30 * time.Second,
		DeliverExternalDeadline:     20 * time.Second,
		ObserveExternalDeadline:     10 * time.Second,
		VerifyExternalDeadline:      10 * time.Second,
		InvestigateLease:            90 * time.Second,
		DeliverLease:                60 * time.Second,
		ObserveLease:                45 * time.Second,
		VerifyLease:                 45 * time.Second,
		InvestigateHeartbeat:        20 * time.Second,
		DeliverHeartbeat:            15 * time.Second,
		ObserveHeartbeat:            10 * time.Second,
		VerifyHeartbeat:             10 * time.Second,
		DrainTimeout:                45 * time.Second,
		ExitDeadline:                55 * time.Second,
	}
}

// LoadAsyncWorkerConfig reads optional duration/pool overrides while retaining
// the frozen defaults. Invalid environment values fail closed at startup.
func LoadAsyncWorkerConfig() (AsyncWorkerConfig, error) {
	cfg := DefaultAsyncWorkerConfig()
	var err error
	if cfg.WorkerID, err = envString("ASYNC_WORKER_ID", cfg.WorkerID); err != nil {
		return AsyncWorkerConfig{}, err
	}
	for _, item := range []struct {
		name  string
		value *int
	}{
		{"ASYNC_INVESTIGATE_MAX_IN_FLIGHT", &cfg.InvestigateMaxInFlight},
		{"ASYNC_DELIVER_MAX_IN_FLIGHT", &cfg.DeliverMaxInFlight},
		{"ASYNC_OBSERVE_MAX_IN_FLIGHT", &cfg.ObserveMaxInFlight},
		{"ASYNC_VERIFY_MAX_IN_FLIGHT", &cfg.VerifyMaxInFlight},
	} {
		if raw := strings.TrimSpace(os.Getenv(item.name)); raw != "" {
			parsed, parseErr := strconv.Atoi(raw)
			if parseErr != nil {
				return AsyncWorkerConfig{}, fmt.Errorf("%s must be an integer: %w", item.name, parseErr)
			}
			*item.value = parsed
		}
	}
	for _, item := range []struct {
		name  string
		value *time.Duration
	}{
		{"ASYNC_INVESTIGATE_HANDLER_DEADLINE", &cfg.InvestigateHandlerDeadline},
		{"ASYNC_DELIVER_HANDLER_DEADLINE", &cfg.DeliverHandlerDeadline},
		{"ASYNC_OBSERVE_HANDLER_DEADLINE", &cfg.ObserveHandlerDeadline},
		{"ASYNC_VERIFY_HANDLER_DEADLINE", &cfg.VerifyHandlerDeadline},
		{"ASYNC_INVESTIGATE_EXTERNAL_DEADLINE", &cfg.InvestigateExternalDeadline},
		{"ASYNC_DELIVER_EXTERNAL_DEADLINE", &cfg.DeliverExternalDeadline},
		{"ASYNC_OBSERVE_EXTERNAL_DEADLINE", &cfg.ObserveExternalDeadline},
		{"ASYNC_VERIFY_EXTERNAL_DEADLINE", &cfg.VerifyExternalDeadline},
		{"ASYNC_INVESTIGATE_LEASE", &cfg.InvestigateLease},
		{"ASYNC_DELIVER_LEASE", &cfg.DeliverLease},
		{"ASYNC_OBSERVE_LEASE", &cfg.ObserveLease},
		{"ASYNC_VERIFY_LEASE", &cfg.VerifyLease},
		{"ASYNC_INVESTIGATE_HEARTBEAT", &cfg.InvestigateHeartbeat},
		{"ASYNC_DELIVER_HEARTBEAT", &cfg.DeliverHeartbeat},
		{"ASYNC_OBSERVE_HEARTBEAT", &cfg.ObserveHeartbeat},
		{"ASYNC_VERIFY_HEARTBEAT", &cfg.VerifyHeartbeat},
		{"ASYNC_DRAIN_TIMEOUT", &cfg.DrainTimeout},
		{"ASYNC_EXIT_DEADLINE", &cfg.ExitDeadline},
	} {
		if raw := strings.TrimSpace(os.Getenv(item.name)); raw != "" {
			parsed, parseErr := time.ParseDuration(raw)
			if parseErr != nil {
				return AsyncWorkerConfig{}, fmt.Errorf("%s must be a duration: %w", item.name, parseErr)
			}
			*item.value = parsed
		}
	}
	if err := cfg.Validate(); err != nil {
		return AsyncWorkerConfig{}, err
	}
	return cfg, nil
}

func envString(name, fallback string) (string, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		value = fallback
	}
	if len(value) > 128 {
		return "", fmt.Errorf("%s exceeds 128 bytes", name)
	}
	return value, nil
}

// Validate enforces pool isolation and the external < handler < lease and
// heartbeat <= lease/3 relationships required by the runtime contract.
func (c AsyncWorkerConfig) Validate() error {
	if strings.TrimSpace(c.WorkerID) == "" || len(c.WorkerID) > 128 {
		return fmt.Errorf("async worker id is required and must be <=128 bytes")
	}
	for name, value := range map[string]int{
		"investigate": c.InvestigateMaxInFlight,
		"deliver":     c.DeliverMaxInFlight,
		"observe":     c.ObserveMaxInFlight,
		"verify":      c.VerifyMaxInFlight,
	} {
		if value <= 0 {
			return fmt.Errorf("%s max-in-flight must be positive", name)
		}
	}
	if err := validateTiming("investigate", c.InvestigateExternalDeadline, c.InvestigateHandlerDeadline, c.InvestigateLease, c.InvestigateHeartbeat); err != nil {
		return err
	}
	if err := validateTiming("deliver", c.DeliverExternalDeadline, c.DeliverHandlerDeadline, c.DeliverLease, c.DeliverHeartbeat); err != nil {
		return err
	}
	if err := validateTiming("observe", c.ObserveExternalDeadline, c.ObserveHandlerDeadline, c.ObserveLease, c.ObserveHeartbeat); err != nil {
		return err
	}
	if err := validateTiming("verify", c.VerifyExternalDeadline, c.VerifyHandlerDeadline, c.VerifyLease, c.VerifyHeartbeat); err != nil {
		return err
	}
	if c.DrainTimeout <= 0 || c.DrainTimeout > 45*time.Second {
		return fmt.Errorf("async drain timeout must be >0 and <=45s")
	}
	if c.ExitDeadline <= 0 || c.ExitDeadline > 55*time.Second || c.ExitDeadline < c.DrainTimeout {
		return fmt.Errorf("async exit deadline must be >= drain and <=55s")
	}
	return nil
}

func validateTiming(name string, external, handler, lease, heartbeat time.Duration) error {
	if external <= 0 || handler <= 0 || lease <= 0 || heartbeat <= 0 {
		return fmt.Errorf("%s timing values must be positive", name)
	}
	if external >= handler || handler >= lease {
		return fmt.Errorf("%s requires external < handler < lease", name)
	}
	if heartbeat > lease/3 {
		return fmt.Errorf("%s heartbeat must be <= lease/3", name)
	}
	return nil
}
