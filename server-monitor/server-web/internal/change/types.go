package change

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const MaxMetadataBytes = 16 * 1024

var (
	uuidPattern   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	commitPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
	digestPattern = regexp.MustCompile(`^[a-z0-9_+.-]+:[0-9a-fA-F]{32,}$`)
)

type SourceType string

const (
	SourceGitHubCommit SourceType = "github_commit"
	SourcePullRequest  SourceType = "github_pull_request"
	SourceCI           SourceType = "ci"
	SourceImage        SourceType = "image"
	SourceArgoCD       SourceType = "argocd"
)

type Status string

const (
	StatusCandidate Status = "candidate"
	StatusMatched   Status = "matched"
	StatusExcluded  Status = "excluded"
	StatusUnknown   Status = "unknown"
)

type Category string

const (
	CategoryConfirmed Category = "confirmed_match"
	CategoryHigh      Category = "high_confidence"
	CategoryLow       Category = "low_confidence"
	CategoryExcluded  Category = "excluded"
	CategoryNoData    Category = "no_data"
)

type Change struct {
	ID                     uint64
	PublicID               string
	IncidentID             uint64
	SourceType             SourceType
	Repository             string
	RepositoryOwner        string
	CommitSHA              string
	BaseCommitSHA          string
	PullRequestNumber      int64
	WorkflowRunID          int64
	WorkflowName           string
	WorkflowConclusion     string
	ImageRepository        string
	ImageTag               string
	ImageDigest            string
	ImageRevision          string
	ArgoCDApplication      string
	ArgoCDProject          string
	ArgoCDTargetRevision   string
	ArgoCDDeployedRevision string
	Environment            string
	Cluster                string
	Namespace              string
	ServiceName            string
	WorkloadKind           string
	WorkloadName           string
	GitOpsPath             string
	StartedAt              *time.Time
	CompletedAt            *time.Time
	DeployedAt             *time.Time
	Status                 Status
	Category               Category
	ChangeSummary          string
	RiskSummary            string
	CorrelationScore       int
	CorrelationReasons     []string
	Metadata               json.RawMessage
	Truncated              bool
	Degraded               bool
	IdempotencyKey         string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type ListFilter struct {
	SourceType SourceType
	Status     Status
	Category   Category
	Page       int
	PageSize   int
}

type Page struct {
	Items    []Change
	Total    int64
	Page     int
	PageSize int
}

type Repository interface {
	CreateIfAbsent(context.Context, *Change) (bool, error)
	GetByPublicID(context.Context, string) (*Change, error)
	ListByIncident(context.Context, string, ListFilter) (Page, error)
}

func New(incidentID uint64, source SourceType, idempotencyParts ...string) (*Change, error) {
	publicID, err := newUUID()
	if err != nil {
		return nil, fmt.Errorf("%w: public id: %v", ErrInvalidArgument, err)
	}
	item := &Change{
		PublicID:       publicID,
		IncidentID:     incidentID,
		SourceType:     source,
		Status:         StatusCandidate,
		Category:       CategoryLow,
		Metadata:       json.RawMessage(`{}`),
		IdempotencyKey: IdempotencyKey(idempotencyParts...),
	}
	if err := item.Validate(); err != nil {
		return nil, err
	}
	return item, nil
}

func (c *Change) Validate() error {
	if c == nil || c.IncidentID == 0 || !uuidPattern.MatchString(strings.ToLower(c.PublicID)) {
		return ErrInvalidArgument
	}
	switch c.SourceType {
	case SourceGitHubCommit, SourcePullRequest, SourceCI, SourceImage, SourceArgoCD:
	default:
		return errors.Join(ErrInvalidArgument, errors.New("invalid source type"))
	}
	switch c.Status {
	case StatusCandidate, StatusMatched, StatusExcluded, StatusUnknown:
	default:
		return errors.Join(ErrInvalidArgument, errors.New("invalid status"))
	}
	switch c.Category {
	case CategoryConfirmed, CategoryHigh, CategoryLow, CategoryExcluded, CategoryNoData:
	default:
		return errors.Join(ErrInvalidArgument, errors.New("invalid category"))
	}
	if c.CorrelationScore < 0 || c.CorrelationScore > 100 || len(c.CorrelationReasons) > 32 {
		return errors.Join(ErrInvalidArgument, errors.New("invalid correlation"))
	}
	if c.CommitSHA != "" && !commitPattern.MatchString(c.CommitSHA) {
		return errors.Join(ErrInvalidArgument, errors.New("invalid commit sha"))
	}
	if c.ImageDigest != "" && !digestPattern.MatchString(c.ImageDigest) {
		return errors.Join(ErrInvalidArgument, errors.New("invalid image digest"))
	}
	if len(c.IdempotencyKey) != 64 || !json.Valid(c.Metadata) || len(c.Metadata) > MaxMetadataBytes {
		return errors.Join(ErrInvalidArgument, errors.New("invalid idempotency key or metadata"))
	}
	for _, value := range []string{c.Repository, c.RepositoryOwner, c.ImageRepository, c.ArgoCDApplication, c.ArgoCDProject, c.Environment, c.Cluster, c.Namespace, c.ServiceName, c.WorkloadKind, c.WorkloadName, c.GitOpsPath} {
		if len(value) > 512 {
			return errors.Join(ErrInvalidArgument, errors.New("field exceeds bound"))
		}
	}
	if len(c.ChangeSummary) > 4096 || len(c.RiskSummary) > 2048 {
		return errors.Join(ErrInvalidArgument, errors.New("summary exceeds bound"))
	}
	return nil
}

func IdempotencyKey(parts ...string) string {
	normalized := make([]string, 0, len(parts))
	for _, part := range parts {
		normalized = append(normalized, strings.ToLower(strings.TrimSpace(part)))
	}
	sum := sha256.Sum256([]byte(strings.Join(normalized, "\x00")))
	return hex.EncodeToString(sum[:])
}

func ValidCommitSHA(value string) bool {
	return commitPattern.MatchString(strings.TrimSpace(value))
}

func newUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16]), nil
}
