//go:build !windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// secretHost is one "host": a working directory with its own key file.
type secretHost struct {
	t       *testing.T
	bin     string
	workdir string
	keyFile string
}

func newSecretHost(t *testing.T, bin string) *secretHost {
	t.Helper()
	workdir := t.TempDir()
	return &secretHost{t: t, bin: bin, workdir: workdir, keyFile: filepath.Join(workdir, "keys", "host.key")}
}

// run runs the CLI as this host, with input on standard input.
func (h *secretHost) run(input string, args ...string) (stdout, stderr string, code int) {
	h.t.Helper()
	cmd := exec.Command(h.bin, append([]string{"--key-file", h.keyFile}, args...)...)
	cmd.Dir = h.workdir
	cmd.Stdin = strings.NewReader(input)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			h.t.Fatalf("running CLI: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return out.String(), errOut.String(), code
}

func (h *secretHost) keygen() (publicKey string) {
	h.t.Helper()
	if _, stderr, code := h.run("", "secret", "keygen"); code != 0 {
		h.t.Fatalf("keygen failed (exit %d): %s", code, stderr)
	}
	stdout, stderr, code := h.run("", "secret", "public-key")
	if code != 0 {
		h.t.Fatalf("public-key failed (exit %d): %s", code, stderr)
	}
	return strings.TrimSpace(stdout)
}

// lock returns the text `secret lock` prints, e.g. `!locked "..."`.
func (h *secretHost) lock(value string, args ...string) string {
	h.t.Helper()
	stdout, stderr, code := h.run(value+"\n", append([]string{"secret", "lock"}, args...)...)
	if code != 0 {
		h.t.Fatalf("lock failed (exit %d): %s", code, stderr)
	}
	return strings.TrimSpace(stdout)
}

// writeConfig writes a config with one repository, offsite, whose
// credentials are given as YAML values, and returns its path.
func (h *secretHost) writeConfig(repoURL, password, awsSecret string) string {
	h.t.Helper()
	path := filepath.Join(h.workdir, "rest-o-matic.yaml")
	content := `
policies:
  hot: {schedule: hourly, retention: {hourly: 24}}
repositories:
  offsite:
    backend: local
    url: ` + repoURL + `
    password: ` + password + `
    env:
      AWS_ACCESS_KEY_ID: AKIAEXAMPLE
      AWS_SECRET_ACCESS_KEY: ` + awsSecret + `
backups:
  documents:
    source: {paths: ["` + h.workdir + `"]}
    policy: hot
    repositories: [offsite]
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	return path
}

func TestCLI_SecretKeygenLockRevealRoundTrip(t *testing.T) {
	bin := buildBinary(t)
	h := newSecretHost(t, bin)

	stdout, stderr, code := h.run("", "secret", "keygen")
	if code != 0 || !contains(stdout, "public key: age1") || !contains(stdout, h.keyFile) {
		t.Fatalf("keygen (exit %d): stdout=%s stderr=%s", code, stdout, stderr)
	}
	if !contains(stderr, "no recovery key is set up") {
		t.Errorf("expected keygen to warn that no recovery key exists, got: %s", stderr)
	}
	if info, err := os.Stat(h.keyFile); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v, mode %v; want it to exist with mode 0600", err, info.Mode().Perm())
	}

	if _, stderr, code := h.run("", "secret", "keygen"); code == 0 || !contains(stderr, "already exists") {
		t.Errorf("a second keygen should refuse (exit %d): %s", code, stderr)
	}

	publicKey, _, _ := h.run("", "secret", "public-key")
	if !strings.HasPrefix(publicKey, "age1") || strings.Count(publicKey, "\n") != 1 {
		t.Errorf("public-key should print only the key, got: %q", publicKey)
	}

	locked := h.lock("correct horse/battery")
	if !strings.HasPrefix(locked, `!locked "`) || contains(locked, "correct horse") {
		t.Fatalf("lock printed %q, want a locked form without the plain text", locked)
	}
	if again := h.lock("correct horse/battery"); again == locked {
		t.Error("locking the same value twice printed identical output")
	}

	configPath := h.writeConfig("/srv/repo", locked, h.lock("wJalrXUt"))
	if stdout, stderr, code := h.run("", "--config", configPath, "validate"); code != 0 {
		t.Fatalf("validate rejected a config with locked values (exit %d): %s %s", code, stdout, stderr)
	}
	if got, stderr, code := h.run("", "--config", configPath, "secret", "reveal", "offsite"); code != 0 || got != "correct horse/battery\n" {
		t.Errorf("reveal password (exit %d) = %q, stderr: %s", code, got, stderr)
	}
	if got, _, code := h.run("", "--config", configPath, "secret", "reveal", "offsite", "AWS_SECRET_ACCESS_KEY"); code != 0 || got != "wJalrXUt\n" {
		t.Errorf("reveal env value (exit %d) = %q", code, got)
	}
	if stdout, _, code := h.run("", "--config", configPath, "secret", "check"); code != 0 || !contains(stdout, "all 2 locked value(s) can be unlocked") {
		t.Errorf("check (exit %d): %s", code, stdout)
	}
}

func TestCLI_SecretRevealRefusals(t *testing.T) {
	bin := buildBinary(t)
	h := newSecretHost(t, bin)
	h.keygen()
	configPath := h.writeConfig("/srv/repo", "plain-password", h.lock("wJalrXUt"))

	cases := map[string]struct {
		args []string
		want string
	}{
		"plain value":        {[]string{"offsite"}, "password is not locked"},
		"plain env value":    {[]string{"offsite", "AWS_ACCESS_KEY_ID"}, "env AWS_ACCESS_KEY_ID is not locked"},
		"unknown repository": {[]string{"nosuch"}, `no such repository "nosuch"`},
		"unknown env name":   {[]string{"offsite", "NOSUCH"}, `no env value "NOSUCH"`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			stdout, stderr, code := h.run("", append([]string{"--config", configPath, "secret", "reveal"}, c.args...)...)
			if code == 0 || stdout != "" || !contains(stderr, c.want) {
				t.Errorf("got exit %d, stdout %q, stderr %q; want a failure saying %q and nothing printed", code, stdout, stderr, c.want)
			}
		})
	}
}

func TestCLI_SecretLockNeedsSomeKey(t *testing.T) {
	bin := buildBinary(t)
	h := newSecretHost(t, bin)

	stdout, stderr, code := h.run("s3cret\n", "secret", "lock")
	if code == 0 || stdout != "" || !contains(stderr, "secret keygen") {
		t.Fatalf("lock with no key at all: exit %d, stdout %q, stderr %q; want a failure pointing at keygen", code, stdout, stderr)
	}

	h.keygen()
	if stdout, stderr, code := h.run("\n", "secret", "lock"); code == 0 || stdout != "" {
		t.Fatalf("lock of an empty value: exit %d, stdout %q, stderr %q; want a failure", code, stdout, stderr)
	}
}

// A value locked on one host for another's public key is usable there, and
// a host asked to use a value locked only for someone else says so.
func TestCLI_SecretLockForAnotherHost(t *testing.T) {
	bin := buildBinary(t)
	a, b := newSecretHost(t, bin), newSecretHost(t, bin)
	a.keygen()
	bPublic := b.keygen()

	shared := a.lock("shared-pw", "--recipient", bPublic)
	onlyA := a.lock("only-a")

	bConfig := b.writeConfig("/srv/repo", shared, onlyA)
	if got, stderr, code := b.run("", "--config", bConfig, "secret", "reveal", "offsite"); code != 0 || got != "shared-pw\n" {
		t.Fatalf("host b could not reveal a value locked for it (exit %d): %q %s", code, got, stderr)
	}

	stdout, stderr, code := b.run("", "--config", bConfig, "secret", "check")
	if code == 0 {
		t.Fatalf("check should fail when a value is locked for another host, got: %s", stdout)
	}
	if !contains(stdout, "repository offsite: env AWS_SECRET_ACCESS_KEY: cannot be unlocked: not locked for this host's key") {
		t.Errorf("expected check to name the repository, field and reason, got: %s", stdout)
	}
	if contains(stdout, "repository offsite: password") {
		t.Errorf("check reported the password, which this host can unlock: %s", stdout)
	}
	if !contains(stderr, "1 of 2 locked value(s) cannot be unlocked") {
		t.Errorf("expected a summary on stderr, got: %s", stderr)
	}

	if _, stderr, code := a.run("x\n", "secret", "lock", "--recipient", "not-a-key"); code == 0 || !contains(stderr, "not an age public key") {
		t.Errorf("a bad --recipient should be refused (exit %d): %s", code, stderr)
	}
}

// Keys in the recovery-recipients file beside the host key can open every
// value locked on that host, without being named each time.
func TestCLI_SecretRecoveryRecipients(t *testing.T) {
	bin := buildBinary(t)
	host, recovery := newSecretHost(t, bin), newSecretHost(t, bin)
	recoveryPublic := recovery.keygen()

	if err := os.MkdirAll(filepath.Dir(host.keyFile), 0o700); err != nil {
		t.Fatal(err)
	}
	recipients := "# recovery key\n" + recoveryPublic + "\n"
	if err := os.WriteFile(filepath.Join(filepath.Dir(host.keyFile), "recovery-recipients"), []byte(recipients), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr, code := host.run("", "secret", "keygen"); code != 0 || contains(stderr, "no recovery key") {
		t.Fatalf("keygen with a recovery key present (exit %d) should not warn: %s", code, stderr)
	}

	locked := host.lock("correct-horse")
	for name, h := range map[string]*secretHost{"host": host, "recovery": recovery} {
		configPath := h.writeConfig("/srv/repo", locked, "plain")
		if got, stderr, code := h.run("", "--config", configPath, "secret", "reveal", "offsite"); code != 0 || got != "correct-horse\n" {
			t.Errorf("the %s key could not open the value (exit %d): %q %s", name, code, got, stderr)
		}
	}
}

func TestCLI_SecretNoHostKey(t *testing.T) {
	bin := buildBinary(t)
	author, keyless := newSecretHost(t, bin), newSecretHost(t, bin)
	author.keygen()
	configPath := keyless.writeConfig("/srv/repo", author.lock("correct-horse"), "plain")

	if _, stderr, code := keyless.run("", "secret", "public-key"); code == 0 || !contains(stderr, "no host key") || !contains(stderr, "secret keygen") {
		t.Errorf("public-key with no key (exit %d): %s", code, stderr)
	}
	// Validation needs no key.
	if stdout, stderr, code := keyless.run("", "--config", configPath, "validate"); code != 0 {
		t.Errorf("validate should pass with no host key (exit %d): %s %s", code, stdout, stderr)
	}
	// Using the repository does.
	stdout, stderr, code := keyless.run("", "--config", configPath, "run", "documents")
	if code == 0 {
		t.Fatal("expected the job to fail without a host key")
	}
	if !contains(stdout, "repository offsite: backup failed: password is locked, and this host has no key to unlock it") {
		t.Errorf("expected the job output to name the repository, the field and the reason, got: %s %s", stdout, stderr)
	}
}

func TestCLI_ValidateRejectsMalformedLockedValue(t *testing.T) {
	bin := buildBinary(t)
	h := newSecretHost(t, bin)
	configPath := h.writeConfig("/srv/repo", `!locked "pasted wrong"`, "plain")

	_, stderr, code := h.run("", "--config", configPath, "validate")
	if code == 0 || !contains(stderr, `repository "offsite": password is marked !locked but is not a valid locked value`) {
		t.Fatalf("validate (exit %d): %s", code, stderr)
	}
}

// The whole point: a real backup to a real repository whose password is
// only in the config in locked form.
func TestCLI_BackupWithLockedPassword(t *testing.T) {
	requireRestic(t)
	bin := buildBinary(t)
	h := newSecretHost(t, bin)
	h.keygen()

	repoPath := filepath.Join(h.workdir, "repo")
	initCmd := exec.Command("restic", "-r", repoPath, "init")
	initCmd.Env = append(os.Environ(), "RESTIC_PASSWORD=correct-horse")
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("restic init: %v: %s", err, out)
	}
	configPath := h.writeConfig(repoPath, h.lock("correct-horse"), h.lock("unused-by-a-local-repo"))
	if data, _ := os.ReadFile(configPath); contains(string(data), "correct-horse") {
		t.Fatal("the config holds the password in plain text")
	}

	stdout, stderr, code := h.run("", "--config", configPath, "run", "documents")
	if code != 0 || !contains(stdout, "job documents: OK") {
		t.Fatalf("run with a locked password failed (exit %d): %s %s", code, stdout, stderr)
	}
	stdout, stderr, code = h.run("", "--config", configPath, "exec", "offsite", "--", "snapshots")
	if code != 0 || !contains(stdout, "documents") {
		t.Fatalf("exec with a locked password failed (exit %d): %s %s", code, stdout, stderr)
	}
	// Nothing recorded on disk holds the password either.
	state, _ := os.ReadFile(filepath.Join(h.workdir, ".rest-o-matic", "state.json"))
	if contains(string(state), "correct-horse") {
		t.Errorf("the state file holds the password: %s", state)
	}
}

// A locked value is an ordinary age file, so the stock age tool opens it.
func TestCLI_LockedValueOpensWithStockAge(t *testing.T) {
	ageBin, err := exec.LookPath("age")
	if err != nil {
		t.Skip("age is not installed; skipping the interoperability check")
	}
	bin := buildBinary(t)
	h := newSecretHost(t, bin)
	h.keygen()
	locked := strings.TrimSuffix(strings.TrimPrefix(h.lock("correct-horse"), `!locked "`), `"`)

	decode := exec.Command("base64", "-d")
	decode.Stdin = strings.NewReader(locked)
	raw, err := decode.Output()
	if err != nil {
		t.Fatalf("base64 -d: %v", err)
	}
	decrypt := exec.Command(ageBin, "--decrypt", "-i", h.keyFile)
	decrypt.Stdin = bytes.NewReader(raw)
	out, err := decrypt.Output()
	if err != nil || string(out) != "correct-horse" {
		t.Fatalf("age --decrypt = %q, %v; want the plain text", out, err)
	}
}
