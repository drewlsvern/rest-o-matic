package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"filippo.io/age"
)

func newHostKey(t *testing.T) (*HostKey, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rest-o-matic", "host.key")
	if _, err := Generate(path); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	key, err := LoadHostKey(path)
	if err != nil {
		t.Fatalf("LoadHostKey: %v", err)
	}
	return key, path
}

func TestGenerate_CreatesAnOwnerOnlyKeyAndReturnsItsPublicKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rest-o-matic", "host.key")
	publicKey, err := Generate(path)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !strings.HasPrefix(publicKey, "age1") {
		t.Fatalf("got public key %q, want an age public key", publicKey)
	}

	key, err := LoadHostKey(path)
	if err != nil {
		t.Fatalf("LoadHostKey: %v", err)
	}
	if key.PublicKey() != publicKey {
		t.Fatalf("loaded public key %q, want %q", key.PublicKey(), publicKey)
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != want {
				t.Errorf("%s has mode %04o, want %04o", p, got, want)
			}
		}
	}
}

func TestGenerate_NeverOverwritesAnExistingKey(t *testing.T) {
	_, path := newHostKey(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Generate(path); err == nil {
		t.Fatal("expected Generate to refuse when a key already exists")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("the existing key was changed")
	}
}

func TestLoadHostKey_MissingFileIsErrNoHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.key")
	_, err := LoadHostKey(path)
	if !errors.Is(err, ErrNoHostKey) || !strings.Contains(err.Error(), path) {
		t.Fatalf("got %v, want ErrNoHostKey naming the path", err)
	}
}

func TestLoadHostKey_RefusesAKeyOthersCanRead(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no permission bits to check on Windows")
	}
	_, path := newHostKey(t)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadHostKey(path)
	if err == nil || !strings.Contains(err.Error(), "0644") || !strings.Contains(err.Error(), path) {
		t.Fatalf("got %v, want an error naming the file and its permissions", err)
	}
}

func TestLoadHostKey_RejectsAFileThatIsNotAKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "host.key")
	if err := os.WriteFile(path, []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadHostKey(path); err == nil || errors.Is(err, ErrNoHostKey) {
		t.Fatalf("got %v, want an error saying the file is not a valid key", err)
	}
}

func TestLockUnlock_RoundTrip(t *testing.T) {
	key, _ := newHostKey(t)
	const secret = "correct-horse battery/staple=="

	locked, err := Lock(secret, []age.Recipient{key.Recipient()})
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	if strings.Contains(locked, "correct-horse") || strings.ContainsAny(locked, "\n ") {
		t.Fatalf("locked form must be one line without the plain text, got %q", locked)
	}
	if err := WellFormed(locked); err != nil {
		t.Fatalf("WellFormed: %v", err)
	}

	got, err := Unlock(locked, key)
	if err != nil || got != secret {
		t.Fatalf("Unlock = %q, %v; want %q", got, err, secret)
	}
}

func TestLock_EachRecipientCanUnlock(t *testing.T) {
	host, _ := newHostKey(t)
	recovery, _ := newHostKey(t)
	stranger, _ := newHostKey(t)

	locked, err := Lock("s3cret", []age.Recipient{host.Recipient(), recovery.Recipient()})
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	for name, key := range map[string]*HostKey{"host": host, "recovery": recovery} {
		if got, err := Unlock(locked, key); err != nil || got != "s3cret" {
			t.Errorf("%s key: Unlock = %q, %v; want the secret", name, got, err)
		}
	}
	if _, err := Unlock(locked, stranger); !errors.Is(err, ErrNotForThisKey) {
		t.Errorf("a key the value wasn't locked for: got %v, want ErrNotForThisKey", err)
	}
}

func TestLock_SameValueTwiceDiffers(t *testing.T) {
	key, _ := newHostKey(t)
	a, err := Lock("s3cret", []age.Recipient{key.Recipient()})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Lock("s3cret", []age.Recipient{key.Recipient()})
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("locking the same value twice produced identical output")
	}
}

func TestLock_RefusesEmptyValueAndNoRecipients(t *testing.T) {
	key, _ := newHostKey(t)
	if _, err := Lock("", []age.Recipient{key.Recipient()}); err == nil {
		t.Error("expected an empty value to be refused")
	}
	if _, err := Lock("s3cret", nil); err == nil {
		t.Error("expected locking for nobody to be refused")
	}
}

func TestMalformedValues(t *testing.T) {
	key, _ := newHostKey(t)
	for _, bad := range []string{"", "not a locked value", "aGVsbG8gd29ybGQ="} {
		if err := WellFormed(bad); !errors.Is(err, ErrMalformed) {
			t.Errorf("WellFormed(%q) = %v, want ErrMalformed", bad, err)
		}
		if _, err := Unlock(bad, key); !errors.Is(err, ErrMalformed) {
			t.Errorf("Unlock(%q) = %v, want ErrMalformed", bad, err)
		}
	}
}

func TestLoadRecipients(t *testing.T) {
	dir := t.TempDir()
	if got, err := LoadRecipients(filepath.Join(dir, "missing")); err != nil || len(got) != 0 {
		t.Fatalf("a missing file: got %d recipients, %v; want none and no error", len(got), err)
	}

	recovery, _ := newHostKey(t)
	path := filepath.Join(dir, "recovery-recipients")
	content := "# recovery key, private half is in the password manager\n\n" + recovery.PublicKey() + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	recipients, err := LoadRecipients(path)
	if err != nil || len(recipients) != 1 {
		t.Fatalf("got %d recipients, %v; want the one listed", len(recipients), err)
	}
	locked, err := Lock("s3cret", recipients)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Unlock(locked, recovery); err != nil || got != "s3cret" {
		t.Fatalf("the recovery key could not open a value locked for the file's recipients: %q, %v", got, err)
	}
}

func TestParseRecipient(t *testing.T) {
	key, _ := newHostKey(t)
	if _, err := ParseRecipient(key.PublicKey()); err != nil {
		t.Errorf("a real public key was rejected: %v", err)
	}
	if _, err := ParseRecipient("not-a-key"); err == nil {
		t.Error("expected a non-key to be rejected")
	}
}

func TestRecipientsPath_IsBesideTheKey(t *testing.T) {
	got := RecipientsPath(filepath.Join("some", "dir", "custom.key"))
	if want := filepath.Join("some", "dir", "recovery-recipients"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestUnlocker_LoadsTheKeyOnceAndReportsWhyItCannot(t *testing.T) {
	key, path := newHostKey(t)
	locked, err := Lock("s3cret", []age.Recipient{key.Recipient()})
	if err != nil {
		t.Fatal(err)
	}

	u := NewUnlocker(path, nil)
	if got, err := u.Reveal(locked); err != nil || got != "s3cret" {
		t.Fatalf("Reveal = %q, %v", got, err)
	}
	// The key is held in memory after first use.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if got, err := u.Reveal(locked); err != nil || got != "s3cret" {
		t.Fatalf("Reveal after the key file was removed = %q, %v; want it served from memory", got, err)
	}

	if _, err := NewUnlocker(path, nil).Reveal(locked); !errors.Is(err, ErrNoHostKey) {
		t.Errorf("with no key file: got %v, want ErrNoHostKey", err)
	}
	pathErr := errors.New("no home directory")
	if _, err := NewUnlocker("", pathErr).Reveal(locked); !errors.Is(err, pathErr) {
		t.Errorf("with an unknown key location: got %v, want that reason", err)
	}
}
