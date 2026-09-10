package config

import "testing"

func TestJobsReferencing(t *testing.T) {
	path := writeTempConfig(t, `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /tmp/nas}
  offsite: {backend: local, url: /tmp/offsite}
  unused: {backend: local, url: /tmp/unused}
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [nas, offsite]
  postgres:
    source: {paths: ["/b"]}
    policy: hot
    repositories: [nas]
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	tests := []struct {
		repo string
		want []string
	}{
		{"nas", []string{"documents", "postgres"}},
		{"offsite", []string{"documents"}},
		{"unused", nil},
		{"does-not-exist", nil},
	}
	for _, tt := range tests {
		got := cfg.JobsReferencing(tt.repo)
		if len(got) != len(tt.want) {
			t.Errorf("JobsReferencing(%q) = %v, want %v", tt.repo, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("JobsReferencing(%q) = %v, want %v", tt.repo, got, tt.want)
				break
			}
		}
	}
}
