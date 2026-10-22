//go:build !windows

package execution

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

func TestBackup_ExclusionArguments(t *testing.T) {
	for name, tc := range map[string]struct {
		src  config.Source
		want string
	}{
		"none": {config.Source{Paths: []string{"/src"}}, "backup /src --tag docs --json"},
		"patterns in order": {config.Source{Paths: []string{"/src"}, Exclude: []string{"/src/b", "Trash", "/src/a"}},
			"backup /src --exclude /src/b --exclude Trash --exclude /src/a --tag docs --json"},
		"caches only":         {config.Source{Paths: []string{"/src"}, ExcludeCaches: true}, "backup /src --exclude-caches --tag docs --json"},
		"one filesystem only": {config.Source{Paths: []string{"/src"}, OneFileSystem: true}, "backup /src --one-file-system --tag docs --json"},
		"everything": {config.Source{Paths: []string{"/a", "/b"}, Exclude: []string{"x"}, ExcludeCaches: true, OneFileSystem: true},
			"backup /a /b --exclude x --exclude-caches --one-file-system --tag docs --json"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newReadModeFakes(t)
			if _, err := f.runner().Backup(context.Background(), f.repo(), config.ReadDirect, tc.src, []string{"docs"}, nil, nil); err != nil {
				t.Fatalf("Backup: %v", err)
			}
			if got, want := f.backupCall(t), "-r "+f.repo().URL+" "+tc.want; got != want {
				t.Fatalf("restic args = %q, want %q", got, want)
			}
		})
	}
}

// Every repository a job backs up to, in either read mode, gets the job's
// exclusions.
func TestRunJob_ExclusionsReachEveryBackup(t *testing.T) {
	for _, readAs := range []string{"", config.ReadPodmanUnshare} {
		t.Run("read_as="+readAs, func(t *testing.T) {
			f := newReadModeFakes(t)
			offsite := f.repo()
			offsite.URL = filepath.Join(f.dir, "offsite")
			cfg := &config.Config{
				Repositories: map[string]config.Repository{"nas": f.repo(), "offsite": offsite},
				Policies:     map[string]config.Policy{"hot": {Schedule: "hourly", Retention: config.Retention{"hourly": 1}}},
				Backups: map[string]config.Job{
					"docs": {Name: "docs", Policy: "hot", ReadAs: readAs,
						Source:       config.Source{Paths: []string{"/src"}, Exclude: []string{"/src/Trash", "Cache"}, OneFileSystem: true},
						Repositories: []config.RepositoryRef{{Name: "nas"}, {Name: "offsite"}}},
				},
			}
			res := RunJob(context.Background(), context.Background(), cfg, "docs", Options{Restic: f.runner(), LockDir: t.TempDir()})
			if len(res.Repos) != 2 || res.Repos[0].BackupErr != nil || res.Repos[1].BackupErr != nil {
				t.Fatalf("expected both backups to succeed, got %+v", res.Repos)
			}

			const want = " backup /src --exclude /src/Trash --exclude Cache --one-file-system --tag docs "
			var backups []string
			for _, c := range f.calls(t, "restic") {
				if strings.Contains(c, " backup ") {
					backups = append(backups, c)
					if !strings.Contains(c, want) {
						t.Errorf("restic call %q doesn't carry the exclusions %q", c, want)
					}
				}
				if strings.Contains(c, " forget ") && strings.Contains(c, "--exclude") {
					t.Errorf("forget was given exclusions: %q", c)
				}
			}
			if len(backups) != 2 {
				t.Fatalf("expected two backups, got %q", backups)
			}
			if readAs == config.ReadPodmanUnshare {
				for _, c := range f.calls(t, "podman") {
					if strings.Contains(c, " backup ") && !strings.Contains(c, want) {
						t.Errorf("wrapped call %q doesn't carry the exclusions", c)
					}
				}
			}
		})
	}
}
