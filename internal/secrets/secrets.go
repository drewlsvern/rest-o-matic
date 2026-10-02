// Package secrets locks and unlocks individual config values with age
// (https://age-encryption.org), so that a config file can leave the host
// without exposing the repository passwords and storage credentials in it.
//
// A locked value is an age-encrypted file, base64-encoded onto one line.
// The host key is a standard age identity file and the recovery recipients
// file a standard age recipients file, so everything here can also be read
// and written with the stock age tools.
package secrets

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
)

// Tag is the YAML tag that marks a config value as locked.
const Tag = "!locked"

const (
	keyFileName        = "host.key"
	recipientsFileName = "recovery-recipients"
	ageHeader          = "age-encryption.org/v1\n"
)

// Errors a caller may need to tell apart.
var (
	// ErrNoHostKey means there is no key file at the host key path.
	ErrNoHostKey = errors.New("no host key")
	// ErrNotForThisKey means the value is a valid locked value, but was
	// locked for other keys.
	ErrNotForThisKey = errors.New("not locked for this host's key")
	// ErrMalformed means the text is not a locked value at all.
	ErrMalformed = errors.New("not a valid locked value")
)

// DefaultKeyPath is where the host key lives unless --key-file says
// otherwise: in the user's config directory, not the state directory,
// because state is disposable and a deleted key would make every locked
// value unreadable.
func DefaultKeyPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("finding the user config directory for the host key (use --key-file): %w", err)
	}
	return filepath.Join(dir, "rest-o-matic", keyFileName), nil
}

// RecipientsPath is the recovery recipients file that belongs with the host
// key at keyPath.
func RecipientsPath(keyPath string) string {
	return filepath.Join(filepath.Dir(keyPath), recipientsFileName)
}

// Generate creates a new host key at keyPath, readable only by its owner,
// and returns its public key. It never replaces an existing file.
func Generate(keyPath string) (publicKey string, err error) {
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return "", fmt.Errorf("generating host key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", filepath.Dir(keyPath), err)
	}
	// O_EXCL is what guarantees an existing key is never overwritten.
	f, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("a host key already exists at %s; it was left unchanged", keyPath)
	}
	if err != nil {
		return "", fmt.Errorf("creating host key: %w", err)
	}
	publicKey = identity.Recipient().String()
	// The same layout age-keygen writes.
	_, err = fmt.Fprintf(f, "# created: %s\n# public key: %s\n%s\n", time.Now().Format(time.RFC3339), publicKey, identity)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(keyPath)
		return "", fmt.Errorf("writing host key: %w", err)
	}
	return publicKey, nil
}

// HostKey is a loaded host key.
type HostKey struct {
	identities []age.Identity
	recipient  age.Recipient
	publicKey  string
}

// PublicKey is the public half, in the form other hosts lock values for.
func (k *HostKey) PublicKey() string { return k.publicKey }

// Recipient is the public half as an age recipient.
func (k *HostKey) Recipient() age.Recipient { return k.recipient }

// LoadHostKey reads the host key at keyPath. It returns an error wrapping
// ErrNoHostKey when the file doesn't exist, and refuses a key file that
// anyone but its owner can read (see checkKeyMode).
func LoadHostKey(keyPath string) (*HostKey, error) {
	info, err := os.Stat(keyPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w at %s", ErrNoHostKey, keyPath)
	}
	if err != nil {
		return nil, fmt.Errorf("reading host key: %w", err)
	}
	if err := checkKeyMode(keyPath, info); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("reading host key: %w", err)
	}
	identities, err := age.ParseIdentities(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("host key %s is not a valid age identity file: %w", keyPath, err)
	}

	// The first key in the file is the one values are locked for; any
	// others can still unlock.
	key := &HostKey{identities: identities}
	switch id := identities[0].(type) {
	case *age.X25519Identity:
		key.recipient, key.publicKey = id.Recipient(), id.Recipient().String()
	case *age.HybridIdentity:
		key.recipient, key.publicKey = id.Recipient(), id.Recipient().String()
	default:
		return nil, fmt.Errorf("host key %s holds an unsupported kind of key", keyPath)
	}
	return key, nil
}

// LoadRecipients reads an age recipients file: one public key per line,
// with blank lines and # comments ignored. A missing file is no recipients.
func LoadRecipients(path string) ([]age.Recipient, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading recovery recipients: %w", err)
	}
	recipients, err := age.ParseRecipients(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("recovery recipients file %s: %w", path, err)
	}
	return recipients, nil
}

// ParseRecipient parses a single public key, as given to --recipient.
func ParseRecipient(publicKey string) (age.Recipient, error) {
	recipients, err := age.ParseRecipients(strings.NewReader(publicKey))
	if err != nil || len(recipients) != 1 {
		return nil, fmt.Errorf("%q is not an age public key", publicKey)
	}
	return recipients[0], nil
}

// Lock encrypts plaintext so that each of recipients can open it, and
// returns the locked form (without the Tag).
func Lock(plaintext string, recipients []age.Recipient) (string, error) {
	if plaintext == "" {
		return "", errors.New("refusing to lock an empty value")
	}
	if len(recipients) == 0 {
		return "", errors.New("no key to lock the value for")
	}
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, recipients...)
	if err != nil {
		return "", fmt.Errorf("locking value: %w", err)
	}
	if _, err := io.WriteString(w, plaintext); err != nil {
		return "", fmt.Errorf("locking value: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("locking value: %w", err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// decode turns a locked value back into the age file it encodes.
func decode(locked string) ([]byte, error) {
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(locked))
	if err != nil || !bytes.HasPrefix(data, []byte(ageHeader)) {
		return nil, ErrMalformed
	}
	return data, nil
}

// WellFormed reports whether locked looks like a locked value, without
// needing any key. It returns ErrMalformed if not.
func WellFormed(locked string) error {
	_, err := decode(locked)
	return err
}

// Unlock opens a locked value with key. It returns ErrMalformed for text
// that isn't a locked value, and ErrNotForThisKey when it is one that key
// can't open.
func Unlock(locked string, key *HostKey) (string, error) {
	data, err := decode(locked)
	if err != nil {
		return "", err
	}
	r, err := age.Decrypt(bytes.NewReader(data), key.identities...)
	var noMatch *age.NoIdentityMatchError
	if errors.As(err, &noMatch) {
		return "", ErrNotForThisKey
	}
	if err != nil {
		return "", ErrMalformed
	}
	plaintext, err := io.ReadAll(r)
	if err != nil {
		return "", ErrMalformed
	}
	return string(plaintext), nil
}

// Unlocker opens locked values with the host key, which it reads on first
// use and at most once. The zero value is not usable; see NewUnlocker.
type Unlocker struct {
	keyPath string
	pathErr error

	once sync.Once
	key  *HostKey
	err  error
}

// NewUnlocker returns an Unlocker for the host key at keyPath. pathErr, if
// non-nil, is why the key's location couldn't be worked out; it is reported
// only if a locked value is actually met, so configs without locked values
// never depend on it.
func NewUnlocker(keyPath string, pathErr error) *Unlocker {
	return &Unlocker{keyPath: keyPath, pathErr: pathErr}
}

// HostKey returns the host key, loading it if this is the first use.
func (u *Unlocker) HostKey() (*HostKey, error) {
	u.once.Do(func() {
		if u.pathErr != nil {
			u.err = u.pathErr
			return
		}
		u.key, u.err = LoadHostKey(u.keyPath)
	})
	return u.key, u.err
}

// Reveal returns the plain text of a locked value.
func (u *Unlocker) Reveal(locked string) (string, error) {
	key, err := u.HostKey()
	if err != nil {
		return "", err
	}
	return Unlock(locked, key)
}
