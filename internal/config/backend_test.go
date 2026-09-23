package config

import (
	"strings"
	"testing"
)

func TestCheckRepository(t *testing.T) {
	tests := []struct {
		name     string
		repo     Repository
		wantErr  string // substring expected in the single error; "" for none
		wantWarn string // substring expected in the single warning; "" for none
	}{
		{name: "local absolute path", repo: Repository{Backend: "local", URL: "/mnt/nas/repo"}},
		{name: "s3 with prefix", repo: Repository{Backend: "s3", URL: "s3:host/bucket"}},
		{name: "s3 with prefix and https", repo: Repository{Backend: "s3", URL: "s3:https://s3.example.com/bucket"}},
		{name: "sftp with prefix", repo: Repository{Backend: "sftp", URL: "sftp:user@host:/srv/restic"}},
		{name: "rest-server alias", repo: Repository{Backend: "rest-server", URL: "rest:https://host:8000/"}},
		{
			name:    "s3 missing prefix",
			repo:    Repository{Backend: "s3", URL: "d6g2.example.com/my-bucket"},
			wantErr: `backend "s3" requires a url beginning with "s3:"`,
		},
		{
			name:    "s3 https without prefix",
			repo:    Repository{Backend: "s3", URL: "https://s3.example.com/my-bucket"},
			wantErr: `requires a url beginning with "s3:"`,
		},
		{
			name:    "url scheme contradicts backend",
			repo:    Repository{Backend: "s3", URL: "sftp:user@host:/srv/restic"},
			wantErr: `backend "s3" requires a url beginning with "s3:", but url begins with "sftp:"`,
		},
		{
			name:    "rest-server alias with wrong scheme",
			repo:    Repository{Backend: "rest-server", URL: "s3:host/bucket"},
			wantErr: `requires a url beginning with "rest:"`,
		},
		{
			name:    "local with remote url",
			repo:    Repository{Backend: "local", URL: "s3:host/bucket"},
			wantErr: `backend "local" but url begins with "s3:"`,
		},
		{
			name:    "missing backend",
			repo:    Repository{URL: "/mnt/nas/repo"},
			wantErr: "backend is required",
		},
		{
			name:     "unrecognised backend",
			repo:     Repository{Backend: "s33", URL: "s3:host/bucket"},
			wantWarn: `unrecognised backend "s33"`,
		},
		{
			name:     "relative local path",
			repo:     Repository{Backend: "local", URL: "backups/restic"},
			wantWarn: "relative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs, warns := CheckRepository("repo", tt.repo)

			if tt.wantErr == "" && len(errs) != 0 {
				t.Fatalf("expected no errors, got %v", errs)
			}
			if tt.wantErr != "" {
				if len(errs) != 1 || !strings.Contains(errs[0].Error(), tt.wantErr) {
					t.Fatalf("expected one error containing %q, got %v", tt.wantErr, errs)
				}
				if !strings.HasPrefix(errs[0].Error(), `repository "repo": `) {
					t.Fatalf("expected error to identify the repository, got %q", errs[0])
				}
			}

			if tt.wantWarn == "" && len(warns) != 0 {
				t.Fatalf("expected no warnings, got %v", warns)
			}
			if tt.wantWarn != "" {
				if len(warns) != 1 || !strings.Contains(warns[0].String(), tt.wantWarn) {
					t.Fatalf("expected one warning containing %q, got %v", tt.wantWarn, warns)
				}
				if !strings.HasPrefix(warns[0].String(), `repository "repo": `) {
					t.Fatalf("expected warning to identify the repository, got %q", warns[0])
				}
			}
		})
	}
}
