package incidentv3mysql

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const maxIngressRejections = 100

// RejectionInput is a bounded, identity-only audit fact for a structurally
// valid Alertmanager alert that cannot map to a server-owned target.
type RejectionInput struct {
	Source           string
	SourceEventID    string
	Fingerprint      string
	AlertInstanceKey string
	CorrelationKey   string
	ReasonCode       string
	Details          map[string]string
}

// RecordRejections persists an entire semantic-rejection subset atomically.
// The deterministic key makes whole-envelope retries harmless.
func (s *Store) RecordRejections(ctx context.Context, rejections []RejectionInput) error {
	if len(rejections) == 0 || len(rejections) > maxIngressRejections {
		return fmt.Errorf("rejection batch must contain 1..%d alerts", maxIngressRejections)
	}
	prepared := make([]preparedRejection, 0, len(rejections))
	for _, rejection := range rejections {
		if err := validateRejection(rejection); err != nil {
			return err
		}
		details, err := json.Marshal(rejection.Details)
		if err != nil {
			return err
		}
		if len(details) > 8*1024 {
			return errors.New("rejection details exceed schema bounds")
		}
		prepared = append(prepared, preparedRejection{input: rejection, details: details})
	}
	sort.Slice(prepared, func(i, j int) bool {
		left, right := prepared[i].input, prepared[j].input
		if left.Source != right.Source {
			return left.Source < right.Source
		}
		if left.SourceEventID != right.SourceEventID {
			return left.SourceEventID < right.SourceEventID
		}
		return left.ReasonCode < right.ReasonCode
	})
	var err error
	for attempt := 1; attempt <= maxTransactionAttempts; attempt++ {
		err = s.recordRejectionsOnce(ctx, prepared)
		if err == nil || !retryableTransactionError(err) || attempt == maxTransactionAttempts {
			return err
		}
		timer := time.NewTimer(time.Duration(attempt) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

type preparedRejection struct {
	input   RejectionInput
	details []byte
}

func (s *Store) recordRejectionsOnce(ctx context.Context, rejections []preparedRejection) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, rejection := range rejections {
		input := rejection.input
		_, err = tx.ExecContext(ctx, `
INSERT IGNORE INTO signal_rejections
    (public_id, source, source_event_id, fingerprint, alert_instance_key, correlation_key,
     reason_code, dedupe_key, payload_hash, details_json, received_at)
VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, ?, ?, NOW(6))`,
			uuid.NewString(), input.Source, input.SourceEventID, input.Fingerprint,
			input.AlertInstanceKey, input.CorrelationKey, input.ReasonCode,
			hashCanonical("rejection", input.Source, input.SourceEventID, input.ReasonCode),
			hashCanonical("payload", input.SourceEventID, string(rejection.details)), rejection.details)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validateRejection(rejection RejectionInput) error {
	for name, value := range map[string]string{
		"source": rejection.Source, "source event id": rejection.SourceEventID,
		"fingerprint": rejection.Fingerprint, "alert instance key": rejection.AlertInstanceKey,
		"reason code": rejection.ReasonCode,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("rejection %s is required", name)
		}
	}
	if len(rejection.Source) > 64 || len(rejection.SourceEventID) > 67 || len(rejection.Fingerprint) > 128 ||
		len(rejection.AlertInstanceKey) != 64 || len(rejection.CorrelationKey) > 67 || len(rejection.ReasonCode) > 64 {
		return errors.New("rejection identity exceeds schema bounds")
	}
	if rejection.Source != "alertmanager" || !strings.HasPrefix(rejection.SourceEventID, "v2:") {
		return errors.New("rejection source identity is not canonical Alertmanager v2")
	}
	for name, digest := range map[string]string{
		"source event id":    strings.TrimPrefix(rejection.SourceEventID, "v2:"),
		"alert instance key": rejection.AlertInstanceKey,
	} {
		if len(digest) != 64 {
			return fmt.Errorf("rejection %s must be a SHA-256 digest", name)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return fmt.Errorf("rejection %s: %w", name, err)
		}
	}
	if len(rejection.Details) != 2 {
		return errors.New("rejection details must contain only labels_hash and status")
	}
	for key, value := range rejection.Details {
		switch key {
		case "labels_hash":
			if len(value) != 64 {
				return errors.New("rejection labels_hash must be a SHA-256 digest")
			}
			if _, err := hex.DecodeString(value); err != nil {
				return fmt.Errorf("rejection labels_hash: %w", err)
			}
		case "status":
			if value != "firing" && value != "resolved" {
				return errors.New("rejection status must be firing or resolved")
			}
		default:
			return errors.New("rejection details contain a non-allowlisted key")
		}
	}
	switch rejection.ReasonCode {
	case "target_not_allowlisted", "target_selector_ambiguous", "target_label_conflict":
		return nil
	default:
		return errors.New("rejection reason is not allowlisted")
	}
}
