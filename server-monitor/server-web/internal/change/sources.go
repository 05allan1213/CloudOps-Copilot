package change

import (
	"context"
	"time"
)

type RepositoryRef struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

func (r RepositoryRef) FullName() string { return r.Owner + "/" + r.Name }

type Commit struct {
	Repository  string    `json:"repository"`
	SHA         string    `json:"sha"`
	Parents     []string  `json:"parents"`
	Message     string    `json:"message"`
	AuthorAt    time.Time `json:"author_at"`
	CommitterAt time.Time `json:"committer_at"`
	HTMLURL     string    `json:"html_url"`
}

type FileChange struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Previous  string `json:"previous_filename,omitempty"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Changes   int    `json:"changes"`
	Patch     string `json:"patch,omitempty"`
	Binary    bool   `json:"binary"`
	Submodule bool   `json:"submodule"`
	Redacted  bool   `json:"redacted"`
	Truncated bool   `json:"truncated"`
}

type DiffSummary struct {
	Files       []FileChange `json:"files"`
	TotalFiles  int          `json:"total_files"`
	Additions   int          `json:"additions"`
	Deletions   int          `json:"deletions"`
	ResultHash  string       `json:"result_hash"`
	ExternalURL string       `json:"external_url"`
	Truncated   bool         `json:"truncated"`
	Redactions  []string     `json:"redactions"`
}

type PullRequest struct {
	Repository     string     `json:"repository"`
	Number         int64      `json:"number"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	State          string     `json:"state"`
	Merged         bool       `json:"merged"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	BaseSHA        string     `json:"base_sha"`
	HeadSHA        string     `json:"head_sha"`
	MergedAt       *time.Time `json:"merged_at"`
	HTMLURL        string     `json:"html_url"`
}

type CheckRun struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
}

type WorkflowRun struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	HeadSHA    string    `json:"head_sha"`
	HeadBranch string    `json:"head_branch"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	HTMLURL    string    `json:"html_url"`
}

type CIStatus struct {
	CommitSHA    string        `json:"commit_sha"`
	CheckRuns    []CheckRun    `json:"check_runs"`
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
	Conclusion   string        `json:"conclusion"`
	Degraded     bool          `json:"degraded"`
}

type GitHubReader interface {
	GetCommit(context.Context, RepositoryRef, string) (Commit, error)
	GetCommitDiff(context.Context, RepositoryRef, string) (DiffSummary, error)
	ListPullRequestsForCommit(context.Context, RepositoryRef, string) ([]PullRequest, error)
	GetPullRequest(context.Context, RepositoryRef, int64) (PullRequest, error)
	GetPullRequestFiles(context.Context, RepositoryRef, int64) (DiffSummary, error)
	GetCIStatus(context.Context, RepositoryRef, string) (CIStatus, error)
}

type ArgoResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Health    string `json:"health"`
	OutOfSync bool   `json:"out_of_sync"`
	Redacted  bool   `json:"redacted"`
}

type ArgoHistory struct {
	ID         int64     `json:"id"`
	Revision   string    `json:"revision"`
	DeployedAt time.Time `json:"deployed_at"`
	SourceRepo string    `json:"source_repo"`
	SourcePath string    `json:"source_path"`
}

type ArgoApplication struct {
	Name              string         `json:"name"`
	Project           string         `json:"project"`
	DestinationServer string         `json:"destination_server"`
	Namespace         string         `json:"namespace"`
	Repository        string         `json:"repository"`
	Path              string         `json:"path"`
	TargetRevision    string         `json:"target_revision"`
	DeployedRevision  string         `json:"deployed_revision"`
	SyncStatus        string         `json:"sync_status"`
	HealthStatus      string         `json:"health_status"`
	OperationPhase    string         `json:"operation_phase"`
	OperationMessage  string         `json:"operation_message"`
	LastSyncedAt      *time.Time     `json:"last_synced_at"`
	History           []ArgoHistory  `json:"history"`
	Resources         []ArgoResource `json:"resources"`
	ExternalURL       string         `json:"external_url"`
	ResultHash        string         `json:"result_hash"`
	Truncated         bool           `json:"truncated"`
	Degraded          bool           `json:"degraded"`
	Unknowns          []string       `json:"unknowns"`
}

type ArgoCDReader interface {
	GetApplication(context.Context, string, string) (ArgoApplication, error)
	GetResourceStatus(context.Context, string, string) ([]ArgoResource, bool, string, error)
}

type ContainerRuntime struct {
	ContainerName      string            `json:"container_name"`
	Image              string            `json:"image"`
	ImageDigest        string            `json:"image_digest"`
	WorkloadKind       string            `json:"workload_kind"`
	WorkloadName       string            `json:"workload_name"`
	Namespace          string            `json:"namespace"`
	DeploymentRevision string            `json:"deployment_revision"`
	Labels             map[string]string `json:"labels,omitempty"`
	Annotations        map[string]string `json:"annotations,omitempty"`
}

type RuntimeReader interface {
	ResolveRuntime(context.Context, string, string, string) ([]ContainerRuntime, error)
}

type RegistryIntegrityStatus string

const (
	RegistryIntegrityVerified RegistryIntegrityStatus = "verified"
	RegistryIntegrityUnknown  RegistryIntegrityStatus = "unknown"
	RegistryIntegrityInvalid  RegistryIntegrityStatus = "invalid"
)

type RegistryRedaction struct {
	AuthMaterialOmitted bool   `json:"auth_material_omitted"`
	ResponsesOmitted    bool   `json:"responses_omitted"`
	Policy              string `json:"policy"`
}

// RegistryMetadata is the bounded result of reading an immutable manifest and
// its config blob. It intentionally contains neither raw provider responses nor
// any authentication material.
type RegistryMetadata struct {
	RegistryID        string                  `json:"registry_id"`
	Repository        string                  `json:"repository"`
	ManifestDigest    string                  `json:"manifest_digest"`
	ConfigDigest      string                  `json:"config_digest"`
	ManifestMediaType string                  `json:"manifest_media_type"`
	ConfigMediaType   string                  `json:"config_media_type"`
	Revision          string                  `json:"revision"`
	Source            string                  `json:"source"`
	Version           string                  `json:"version"`
	ReadAt            time.Time               `json:"read_at"`
	Integrity         RegistryIntegrityStatus `json:"integrity_status"`
	ResultHash        string                  `json:"result_hash"`
	Valid             bool                    `json:"valid"`
	Truncated         bool                    `json:"truncated"`
	Degraded          bool                    `json:"degraded"`
	Redaction         RegistryRedaction       `json:"redaction"`
}

type RegistryMetadataReader interface {
	ReadMetadata(context.Context, string, string) (RegistryMetadata, error)
}
