//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// setRecovery makes publicKey this host's only recovery key.
func (h *secretHost) setRecovery(publicKey string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.keyFile), 0o700); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(h.keyFile), "recovery-recipients"), []byte(publicKey+"\n"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// reveals checks that h can open every locked value in a config written
// by writeRelockConfig.
func (h *secretHost) reveals(configPath, who string) {
	h.t.Helper()
	for args, want := range map[string]string{"offsite": "pw-one", "offsite AWS_SECRET_ACCESS_KEY": "aws-two", "nas": "pw-one"} {
		got, stderr, code := h.run("", append([]string{"--config", configPath, "secret", "reveal"}, strings.Fields(args)...)...)
		if code != 0 || got != want+"\n" {
			h.t.Errorf("%s: reveal %s (exit %d): got %q, want %q: %s", who, args, code, got, want, stderr)
		}
	}
}

// relockConfig writes a config holding two locked values, one shared
// through an anchor, among comments, blank lines and inline maps.
func writeRelockConfig(t *testing.T, h *secretHost, password, aws string) string {
	t.Helper()
	path := filepath.Join(h.workdir, "rest-o-matic.yaml")
	content := `# rest-o-matic config for this host

x-shared:
  password: &pw ` + password + `   # shared by both

policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  nas: {backend: local, url: /srv/nas, password: *pw}
  offsite:
    backend: local
    url: /srv/repo
    password: *pw
    env:
      AWS_ACCESS_KEY_ID: AKIAEXAMPLE
      AWS_SECRET_ACCESS_KEY: ` + aws + `
      MY_PREFIX: !plain "host-a/"
backups:
  documents:
    source: {paths: ["/a"]}
    policy: hot
    repositories: [offsite]   # not nas
`
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatal(err)
	}
	return path
}

var lockedText = regexp.MustCompile(`!locked "[^"]*"`)

func TestCLI_SecretRelockAddsRecoveryKey(t *testing.T) {
	bin := buildBinary(t)
	host, recovery := newSecretHost(t, bin), newSecretHost(t, bin)
	host.keygen()
	configPath := writeRelockConfig(t, host, host.lock("pw-one"), host.lock("aws-two"))
	before, _ := os.ReadFile(configPath)

	// Added after the values were locked: it can't open them yet.
	host.setRecovery(recovery.keygen())
	if _, _, code := recovery.run("", "--config", configPath, "secret", "reveal", "offsite"); code == 0 {
		t.Fatal("the recovery key opened a value locked before it was added")
	}

	stdout, stderr, code := host.run("", "--config", configPath, "secret", "relock")
	if code != 0 || !contains(stdout, "re-locked 2 value(s) for this host's key and 1 recovery key(s)") {
		t.Fatalf("relock (exit %d): stdout=%s stderr=%s", code, stdout, stderr)
	}
	if out := stdout + stderr; contains(out, "pw-one") || contains(out, "aws-two") {
		t.Errorf("relock printed a plain value: %s", out)
	}
	if contains(stderr, "no recovery key") {
		t.Errorf("relock warned about a missing recovery key that exists: %s", stderr)
	}
	host.reveals(configPath, "host")
	recovery.reveals(configPath, "recovery key")

	after, _ := os.ReadFile(configPath)
	if got, want := lockedText.ReplaceAll(after, nil), lockedText.ReplaceAll(before, nil); string(got) != string(want) {
		t.Errorf("relock changed more than the locked values:\n--- before\n%s\n--- after\n%s", before, after)
	}
	if string(after) == string(before) {
		t.Error("relock did not change the locked values")
	}
	if info, err := os.Stat(configPath); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("config mode after relock: %v %v, want 0640", info.Mode().Perm(), err)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(host.workdir, ".*.tmp")); len(leftovers) != 0 {
		t.Errorf("relock left temporary files: %v", leftovers)
	}
}

func TestCLI_SecretRelockReplacementHost(t *testing.T) {
	bin := buildBinary(t)
	old, recovery, replacement := newSecretHost(t, bin), newSecretHost(t, bin), newSecretHost(t, bin)
	recoveryPublic := recovery.keygen()
	old.setRecovery(recoveryPublic)
	old.keygen()
	oldConfig := writeRelockConfig(t, old, old.lock("pw-one"), old.lock("aws-two"))

	data, _ := os.ReadFile(oldConfig)
	configPath := filepath.Join(replacement.workdir, "rest-o-matic.yaml")
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	replacement.setRecovery(recoveryPublic)
	replacement.keygen()
	if _, _, code := replacement.run("", "--config", configPath, "secret", "reveal", "offsite"); code == 0 {
		t.Fatal("the replacement host opened a value locked for the old one")
	}

	if stdout, stderr, code := replacement.run("", "--config", configPath, "secret", "relock", "--with-key", recovery.keyFile); code != 0 {
		t.Fatalf("relock --with-key (exit %d): stdout=%s stderr=%s", code, stdout, stderr)
	}
	replacement.reveals(configPath, "replacement host")
	recovery.reveals(configPath, "recovery key")
}

func TestCLI_SecretRelockWritesNothingOnFailure(t *testing.T) {
	bin := buildBinary(t)
	host, stranger := newSecretHost(t, bin), newSecretHost(t, bin)
	host.keygen()
	stranger.keygen()
	configPath := writeRelockConfig(t, host, host.lock("pw-one"), stranger.lock("aws-two"))
	before, _ := os.ReadFile(configPath)

	stdout, stderr, code := host.run("", "--config", configPath, "secret", "relock")
	if code == 0 {
		t.Fatalf("relock succeeded with a value it can't open: %s", stdout)
	}
	if !contains(stdout, `repository "offsite": env AWS_SECRET_ACCESS_KEY: cannot be opened`) || contains(stdout, `"nas"`) {
		t.Errorf("relock should name only the value it can't open, got: %s", stdout)
	}
	if !contains(stderr, "1 of 2 locked value(s) cannot be opened; the config was not changed") {
		t.Errorf("unexpected error: %s", stderr)
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(before) {
		t.Error("relock changed the config although a value couldn't be opened")
	}
}

func TestCLI_SecretRelockDryRun(t *testing.T) {
	bin := buildBinary(t)
	host := newSecretHost(t, bin)
	host.keygen()
	configPath := writeRelockConfig(t, host, host.lock("pw-one"), host.lock("aws-two"))
	before, _ := os.ReadFile(configPath)

	stdout, stderr, code := host.run("", "--config", configPath, "secret", "relock", "--dry-run")
	if code != 0 || !contains(stdout, "would re-lock 2 value(s) for this host's key and 0 recovery key(s)") {
		t.Fatalf("relock --dry-run (exit %d): stdout=%s stderr=%s", code, stdout, stderr)
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(before) {
		t.Error("a dry run changed the config")
	}
}

func TestCLI_SecretRelockNeedsHostKey(t *testing.T) {
	bin := buildBinary(t)
	host, recovery := newSecretHost(t, bin), newSecretHost(t, bin)
	recovery.keygen()
	configPath := writeRelockConfig(t, host, recovery.lock("pw-one"), recovery.lock("aws-two"))
	before, _ := os.ReadFile(configPath)

	_, stderr, code := host.run("", "--config", configPath, "secret", "relock", "--with-key", recovery.keyFile)
	if code == 0 || !contains(stderr, "secret keygen") {
		t.Fatalf("relock without a host key (exit %d) should fail and point at keygen: %s", code, stderr)
	}
	if after, _ := os.ReadFile(configPath); string(after) != string(before) {
		t.Error("relock changed the config without a host key")
	}
}

func TestCLI_SecretRelockNoLockedValues(t *testing.T) {
	bin := buildBinary(t)
	host := newSecretHost(t, bin)
	host.keygen()
	configPath := host.writeConfig("/srv/repo", "plain", "plain")
	if stdout, stderr, code := host.run("", "--config", configPath, "secret", "relock"); code != 0 || !contains(stdout, "no locked values") {
		t.Fatalf("relock (exit %d): stdout=%s stderr=%s", code, stdout, stderr)
	}
}
