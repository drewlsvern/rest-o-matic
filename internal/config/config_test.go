package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "rest-o-matic.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return path
}

// --- Source Declaration ---

func TestSource_ValidPathsAccepted(t *testing.T) {
	path := writeTempConfig(t, `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /tmp/repo}
backups:
  documents:
    source: {paths: ["~/documents"]}
    policy: hot
    repositories: [nas]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	job := cfg.Backups["documents"]
	if !reflect.DeepEqual(job.Source.Paths, []string{"~/documents"}) {
		t.Fatalf("unexpected source paths: %v", job.Source.Paths)
	}
}

func TestSource_NonPathsTypeRejected(t *testing.T) {
	path := writeTempConfig(t, `
backups:
  postgres:
    source: {container: postgres}
    policy: hot
    repositories: [nas]
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for unsupported source type, got nil")
	}
}

func TestSource_MissingPathsRejectedAtValidation(t *testing.T) {
	path := writeTempConfig(t, `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /tmp/repo}
backups:
  documents:
    source: {}
    policy: hot
    repositories: [nas]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	errs := Validate(cfg)
	if !containsJobError(errs, "documents", "source.paths must be non-empty") {
		t.Fatalf("expected empty-paths validation error, got: %v", errs)
	}
}

// --- Repository Declaration ---

func TestRepository_LocalBackendAccepted(t *testing.T) {
	path := writeTempConfig(t, `
repositories:
  nas: {backend: local, url: /mnt/nas/repo}
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Repositories["nas"].URL != "/mnt/nas/repo" {
		t.Fatalf("unexpected repository: %+v", cfg.Repositories["nas"])
	}
}

func TestRepository_NonLocalBackendAccepted(t *testing.T) {
	path := writeTempConfig(t, `
repositories:
  offsite:
    backend: s3
    url: "s3:https://s3.example.com/bucket/repo"
    env:
      AWS_ACCESS_KEY_ID: id
      AWS_SECRET_ACCESS_KEY: secret
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repo := cfg.Repositories["offsite"]
	if repo.Backend != "s3" || repo.Env["AWS_ACCESS_KEY_ID"] != "id" {
		t.Fatalf("unexpected repository: %+v", repo)
	}
}

// --- Named Policy Definition / Job Policy Reference ---

func TestPolicy_ReusedAcrossJobs(t *testing.T) {
	path := writeTempConfig(t, `
policies:
  hot: {schedule: hourly, retention: {hourly: 24, daily: 30}}
repositories:
  nas: {backend: local, url: /tmp/repo}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [nas]
  postgres:
    source: {paths: ["/b"]}
    policy: hot
    repositories: [nas]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	docSched, _ := cfg.EffectiveSchedule("documents")
	pgSched, _ := cfg.EffectiveSchedule("postgres")
	if docSched != "hourly" || pgSched != "hourly" {
		t.Fatalf("expected both jobs to resolve schedule hourly, got %q and %q", docSched, pgSched)
	}
}

func TestJob_UndefinedPolicyRejected(t *testing.T) {
	path := writeTempConfig(t, `
repositories:
  nas: {backend: local, url: /tmp/repo}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: missing
    repositories: [nas]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	errs := Validate(cfg)
	if !containsJobError(errs, "documents", `references undefined policy "missing"`) {
		t.Fatalf("expected undefined-policy validation error, got: %v", errs)
	}
}

// --- Repository Reference Validation ---

func TestJob_UndefinedRepositoryRejected(t *testing.T) {
	path := writeTempConfig(t, `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [missing]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	errs := Validate(cfg)
	if !containsJobError(errs, "documents", `references undefined repository "missing"`) {
		t.Fatalf("expected undefined-repository validation error, got: %v", errs)
	}
}

func TestValidate_ReportsMultipleSimultaneousErrors(t *testing.T) {
	path := writeTempConfig(t, `
backups:
  documents:
    source: {paths: []}
    policy: missing-policy
    repositories: [missing-repo]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	errs := Validate(cfg)
	if len(errs) < 3 {
		t.Fatalf("expected at least 3 validation errors (policy, repo, paths), got %d: %v", len(errs), errs)
	}
}

func containsJobError(errs []error, job, substr string) bool {
	for _, e := range errs {
		ve, ok := e.(ValidationError)
		if !ok {
			continue
		}
		if ve.Job == job && contains(ve.Message, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 || indexOf(s, substr) >= 0)
}

func indexOf(s, substr string) int {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// --- Retention override cascade ---

func TestResolveRetention_NoOverridePassesThroughPolicy(t *testing.T) {
	policy := Retention{"hourly": 24, "daily": 30, "weekly": 12}
	got := ResolveRetention(policy, nil, nil)
	if !reflect.DeepEqual(got, Retention{"hourly": 24, "daily": 30, "weekly": 12}) {
		t.Fatalf("unexpected retention: %v", got)
	}
}

func TestResolveRetention_JobLevelOverrideKeepsUnspecifiedKeys(t *testing.T) {
	policy := Retention{"hourly": 24, "daily": 30, "weekly": 12}
	jobOverride := Retention{"weekly": 8}
	got := ResolveRetention(policy, jobOverride, nil)
	want := Retention{"hourly": 24, "daily": 30, "weekly": 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestResolveRetention_RepoOverrideWinsOverJobAndPolicy(t *testing.T) {
	policy := Retention{"hourly": 24, "daily": 30, "weekly": 12}
	jobOverride := Retention{"weekly": 8}
	repoOverride := Retention{"daily": 30}
	got := ResolveRetention(policy, jobOverride, repoOverride)
	want := Retention{"hourly": 24, "daily": 30, "weekly": 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestEffectiveRetention_IndependentAcrossRepositories(t *testing.T) {
	path := writeTempConfig(t, `
policies:
  hot: {schedule: hourly, retention: {hourly: 24, daily: 30, weekly: 12}}
repositories:
  nas: {backend: local, url: /tmp/nas}
  offsite: {backend: local, url: /tmp/offsite}
backups:
  postgres:
    source: {paths: ["/var/backups/postgres"]}
    policy: hot
    retention: {weekly: 8}
    repositories:
      - nas
      - repo: offsite
        retention: {daily: 10}
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	nasRet, err := cfg.EffectiveRetention("postgres", "nas")
	if err != nil {
		t.Fatalf("EffectiveRetention(nas): %v", err)
	}
	wantNas := Retention{"hourly": 24, "daily": 30, "weekly": 8}
	if !reflect.DeepEqual(nasRet, wantNas) {
		t.Fatalf("nas retention: got %v, want %v", nasRet, wantNas)
	}

	offsiteRet, err := cfg.EffectiveRetention("postgres", "offsite")
	if err != nil {
		t.Fatalf("EffectiveRetention(offsite): %v", err)
	}
	wantOffsite := Retention{"hourly": 24, "daily": 10, "weekly": 8}
	if !reflect.DeepEqual(offsiteRet, wantOffsite) {
		t.Fatalf("offsite retention: got %v, want %v", offsiteRet, wantOffsite)
	}
	if reflect.DeepEqual(nasRet, offsiteRet) {
		t.Fatal("expected nas and offsite to have different effective retention")
	}
}

// --- RepositoryRef mapping form ---

func TestRepositoryRef_MappingFormRequiresRepoName(t *testing.T) {
	path := writeTempConfig(t, `
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories:
      - retention: {daily: 30}
`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for a repository entry missing 'repo', got nil")
	}
}
