package execution

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/drewlsvern/rest-o-matic/internal/config"
)

// A value marked !plain reaches restic exactly as written.
func TestRepoEnv_PlainMarkedValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rest-o-matic.yaml")
	if err := os.WriteFile(path, []byte(`
repositories:
  offsite:
    backend: local
    url: /srv/repo
    password: x
    env:
      MY_BUCKET_PREFIX: !plain "host-a/"
`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	env, err := NewResticRunner().repoEnv(cfg.Repositories["offsite"])
	if err != nil {
		t.Fatalf("repoEnv: %v", err)
	}
	if !slices.Contains(env, "MY_BUCKET_PREFIX=host-a/") {
		t.Fatal("MY_BUCKET_PREFIX=host-a/ is not in restic's environment")
	}
}
