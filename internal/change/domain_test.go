package change

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestChangeValidationPublicIDIdempotencyAndMetadata(t *testing.T) {
	item, err := New(1, SourceGitHubCommit, "Example/Repo", "abcdef1")
	if err != nil {
		t.Fatal(err)
	}
	if !uuidPattern.MatchString(item.PublicID) || len(item.IdempotencyKey) != 64 {
		t.Fatalf("unexpected IDs: %+v", item)
	}
	item.Metadata = json.RawMessage(`{"bounded":true}`)
	if err := item.Validate(); err != nil {
		t.Fatal(err)
	}
	item.Metadata = json.RawMessage(`{"value":"` + strings.Repeat("x", MaxMetadataBytes) + `"}`)
	if err := item.Validate(); err == nil {
		t.Fatal("expected oversized metadata rejection")
	}
	item.Metadata = json.RawMessage(`{}`)
	item.CorrelationScore = 101
	if err := item.Validate(); err == nil {
		t.Fatal("expected score rejection")
	}
}

func TestCorrelationPositiveAndNegativeRules(t *testing.T) {
	incidentAt := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	runtime := RuntimeContext{IncidentPublicID: "incident", FirstSeenAt: incidentAt, Cluster: "prod", Environment: "prod", Namespace: "payments", ServiceName: "checkout", WorkloadName: "checkout", Image: "registry/checkout:v2", ImageDigest: "sha256:" + strings.Repeat("a", 64), Revision: strings.Repeat("b", 40), ArgoApplication: "checkout-prod"}
	deployed := incidentAt.Add(-3 * time.Minute)
	base := Change{PublicID: "positive", ServiceName: "checkout", WorkloadName: "checkout", Namespace: "payments", Environment: "prod", Cluster: "prod", ImageDigest: runtime.ImageDigest, CommitSHA: runtime.Revision, ArgoCDApplication: "checkout-prod", ArgoCDDeployedRevision: runtime.Revision, WorkflowConclusion: "success", DeployedAt: &deployed}
	cases := []struct {
		name   string
		mutate func(*Change)
	}{
		{"other service", func(c *Change) { c.ServiceName, c.WorkloadName = "catalog", "catalog" }},
		{"other namespace", func(c *Change) { c.Namespace = "other" }},
		{"other application", func(c *Change) { c.ArgoCDApplication = "other" }},
		{"revision mismatch", func(c *Change) {
			c.CommitSHA, c.ArgoCDDeployedRevision = strings.Repeat("c", 40), strings.Repeat("c", 40)
		}},
		{"after incident", func(c *Change) { value := incidentAt.Add(time.Minute); c.DeployedAt = &value }},
		{"outside lookback", func(c *Change) { value := incidentAt.Add(-25 * time.Hour); c.DeployedAt = &value }},
		{"digest mismatch", func(c *Change) { c.ImageDigest = "sha256:" + strings.Repeat("d", 64) }},
		{"failed undeployed", func(c *Change) { c.WorkflowConclusion, c.ArgoCDDeployedRevision = "failure", "" }},
	}
	candidates := []Change{base}
	for _, tc := range cases {
		item := base
		item.PublicID = tc.name
		tc.mutate(&item)
		candidates = append(candidates, item)
	}
	result := Correlate(runtime, candidates, 24*time.Hour, incidentAt.Add(time.Hour))
	if len(result.Candidates) != len(candidates) || result.Candidates[0].Category != CategoryConfirmed || result.Candidates[0].Score < 90 {
		t.Fatalf("positive candidate not confirmed: %+v", result.Candidates)
	}
	for _, candidate := range result.Candidates[1:] {
		if !candidate.Excluded {
			t.Fatalf("negative candidate was not excluded: %+v", candidate)
		}
	}
}

func TestCorrelationTimeBoundaryAndMultipleCandidateOrder(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	runtime := RuntimeContext{IncidentPublicID: "incident", FirstSeenAt: now, Namespace: "ns", ServiceName: "svc"}
	start := now.UTC().Add(-time.Hour)
	items := []Change{{PublicID: "b", Namespace: "ns", ServiceName: "svc", DeployedAt: &start}, {PublicID: "a", Namespace: "ns", ServiceName: "svc", DeployedAt: &start}}
	result := Correlate(runtime, items, time.Hour, now)
	if result.LookbackStart != start || result.Candidates[0].ChangeID != "a" || result.Candidates[0].TimeDeltaSeconds != 3600 {
		t.Fatalf("unexpected boundary/order: %+v", result)
	}
}

func TestCorrelationRejectsRevisionPrefixMatching(t *testing.T) {
	now := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	deployed := now.Add(-time.Minute)
	full := strings.Repeat("a", 40)
	runtime := RuntimeContext{FirstSeenAt: now, Namespace: "ns", ServiceName: "svc", Revision: full}
	candidate := Change{PublicID: "short", Namespace: "ns", ServiceName: "svc", ArgoCDDeployedRevision: full[:7], DeployedAt: &deployed}
	result := Correlate(runtime, []Change{candidate}, time.Hour, now)
	if !result.Candidates[0].Excluded || !containsString(result.Candidates[0].Reasons, "revision mismatch") {
		t.Fatalf("revision prefix was accepted: %+v", result.Candidates[0])
	}
}

func TestImageResolutionRequiresCompleteRegistryBackedChain(t *testing.T) {
	revision := strings.Repeat("a", 40)
	digest := "sha256:" + strings.Repeat("b", 64)
	source := "https://github.com/acme/app"
	metadata := RegistryMetadata{ManifestDigest: digest, ConfigDigest: "sha256:" + strings.Repeat("c", 64), Revision: revision, Source: source, Version: revision, Integrity: RegistryIntegrityVerified, Valid: true}
	confirmed := ResolveImageRevision(ImageResolutionInput{RuntimeDigest: digest, RegistryMetadata: metadata, ArgoDeployedRevision: revision, GitHubCommitSHA: revision, ExpectedSourceRepository: source, AllowedOCISources: []string{source}})
	if confirmed.Status != ImageConfirmed || !confirmed.Valid || confirmed.Revision != revision {
		t.Fatalf("complete trusted chain not confirmed: %+v", confirmed)
	}
	unknown := ResolveImageRevision(ImageResolutionInput{ImageTag: "latest"})
	if unknown.Status != ImageUnknown || !containsString(unknown.Reasons, ReasonMutableTagOnly) {
		t.Fatalf("mutable tag must remain unknown: %+v", unknown)
	}
}

func TestOCILabelValidationAndImageMappingEdges(t *testing.T) {
	revision := strings.Repeat("a", 40)
	source := "https://github.com/acme/app"
	labels := map[string]string{"org.opencontainers.image.revision": revision, "org.opencontainers.image.source": source, "org.opencontainers.image.version": revision}
	if result := ValidateOCILabels(labels, []string{source}); !result.Valid || result.Revision != revision || result.Source != source || result.Version != revision {
		t.Fatalf("valid labels rejected: %+v", result)
	}
	for name, mutate := range map[string]func(map[string]string){
		"missing revision": func(v map[string]string) { delete(v, "org.opencontainers.image.revision") },
		"missing source":   func(v map[string]string) { delete(v, "org.opencontainers.image.source") },
		"missing version":  func(v map[string]string) { delete(v, "org.opencontainers.image.version") },
		"wrong source":     func(v map[string]string) { v["org.opencontainers.image.source"] = "https://github.com/other/app" },
		"secret label":     func(v map[string]string) { v["build.token"] = "must-not-exist" },
	} {
		t.Run(name, func(t *testing.T) {
			copy := map[string]string{}
			for key, value := range labels {
				copy[key] = value
			}
			mutate(copy)
			if result := ValidateOCILabels(copy, []string{source}); result.Valid {
				t.Fatalf("invalid labels accepted: %+v", result)
			}
		})
	}
	digestA := "sha256:" + strings.Repeat("b", 64)
	digestB := "sha256:" + strings.Repeat("c", 64)
	metadata := RegistryMetadata{ManifestDigest: digestB, ConfigDigest: digestA, Revision: revision, Source: source, Version: revision, Integrity: RegistryIntegrityVerified, Valid: true}
	digestConflict := ResolveImageRevision(ImageResolutionInput{RuntimeDigest: digestA, RegistryMetadata: metadata, ArgoDeployedRevision: revision, GitHubCommitSHA: revision, ExpectedSourceRepository: source, AllowedOCISources: []string{source}})
	if digestConflict.Status != ImageConflict {
		t.Fatalf("digest conflict was not fail-closed: %+v", digestConflict)
	}
	metadata.ManifestDigest = digestA
	untrustedOCI := ResolveImageRevision(ImageResolutionInput{RuntimeDigest: digestA, RegistryMetadata: metadata, ArgoDeployedRevision: revision, GitHubCommitSHA: revision, ExpectedSourceRepository: source, AllowedOCISources: []string{"https://github.com/other/app"}})
	if untrustedOCI.Status != ImageConflict || !containsString(untrustedOCI.Reasons, ReasonSourceConflict) {
		t.Fatalf("unallowlisted OCI source did not conflict: %+v", untrustedOCI)
	}
	metadata.Source = source
	metadata.Truncated = true
	truncated := ResolveImageRevision(ImageResolutionInput{RuntimeDigest: digestA, RegistryMetadata: metadata, ArgoDeployedRevision: revision, GitHubCommitSHA: revision, ExpectedSourceRepository: source, AllowedOCISources: []string{source}})
	if truncated.Status != ImageUnknown || !truncated.Truncated || !containsString(truncated.Reasons, ReasonMetadataTruncated) {
		t.Fatalf("truncated metadata was trusted: %+v", truncated)
	}
}

func TestImageResolutionConflictsUnknownsAndStableReasonOrder(t *testing.T) {
	revision := strings.Repeat("a", 40)
	digest := "sha256:" + strings.Repeat("b", 64)
	source := "https://github.com/acme/app"
	metadata := RegistryMetadata{ManifestDigest: digest, ConfigDigest: "sha256:" + strings.Repeat("c", 64), Revision: revision, Source: source, Version: revision, Integrity: RegistryIntegrityVerified, Valid: true}
	cases := []struct {
		name   string
		mutate func(*ImageResolutionInput)
		status ImageResolutionStatus
		reason string
	}{
		{"invalid revision", func(v *ImageResolutionInput) {
			v.RegistryMetadata.Revision = "not-a-sha"
			v.RegistryMetadata.Version = "not-a-sha"
		}, ImageUnknown, ReasonRevisionInvalid},
		{"missing labels", func(v *ImageResolutionInput) { v.RegistryMetadata.Revision = ""; v.RegistryMetadata.Version = "" }, ImageUnknown, ReasonLabelsMissing},
		{"revision conflict", func(v *ImageResolutionInput) { v.ArgoDeployedRevision = strings.Repeat("d", 40) }, ImageConflict, ReasonRevisionConflict},
		{"github missing", func(v *ImageResolutionInput) { v.GitHubCommitSHA = "" }, ImageUnknown, ReasonGitHubCommitMissing},
		{"auth failure", func(v *ImageResolutionInput) {
			v.RegistryErrorCode = "authentication"
			v.RegistryMetadata = RegistryMetadata{}
		}, ImageUnknown, ReasonRegistryAuthFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := ImageResolutionInput{RuntimeDigest: digest, RegistryMetadata: metadata, ArgoDeployedRevision: revision, GitHubCommitSHA: revision, ExpectedSourceRepository: source, AllowedOCISources: []string{source}}
			tc.mutate(&input)
			result := ResolveImageRevision(input)
			if result.Status != tc.status || !containsString(result.Reasons, tc.reason) {
				t.Fatalf("unexpected result: %+v", result)
			}
		})
	}
	multi := ResolveImageRevision(ImageResolutionInput{ImageTag: "latest", RegistryErrorCode: "authentication"})
	want := []string{ReasonRuntimeDigestMissing, ReasonRegistryAuthFailed, ReasonMetadataInvalid, ReasonLabelsMissing, ReasonArgoRevisionMissing, ReasonGitHubCommitMissing, ReasonMutableTagOnly}
	if strings.Join(multi.Reasons, ",") != strings.Join(want, ",") {
		t.Fatalf("reason order changed: got=%v want=%v", multi.Reasons, want)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func TestRedactTextRemovesCredentialsAndBoundsProviderText(t *testing.T) {
	input := "Authorization: Bearer top-secret token=another-secret\n-----BEGIN RSA PRIVATE KEY-----\nprivate-material\n-----END RSA PRIVATE KEY-----"
	redacted, changed := RedactText(input, 256)
	if !changed || strings.Contains(redacted, "top-secret") || strings.Contains(redacted, "another-secret") || strings.Contains(redacted, "private-material") {
		t.Fatalf("credential text was not redacted: %q", redacted)
	}
}
