package central

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// enrolmentFileName is the enrolment's file, beside the host key.
const enrolmentFileName = "enrolment.json"

// ErrNotEnrolled means there is no enrolment file.
var ErrNotEnrolled = errors.New("this host is not enrolled with a central app")

// Enrolment is what the host keeps about its central app.
type Enrolment struct {
	URL        string `json:"url"`
	HostID     string `json:"host_id"`
	HostName   string `json:"host_name"`
	Credential string `json:"credential"`
	// ConfigPath is the absolute path of the config file the host was
	// enrolled for. Only that config reports to the central app.
	ConfigPath string    `json:"config_path"`
	CAFile     string    `json:"ca_file,omitempty"`
	AllowHTTP  bool      `json:"allow_http"`
	EnrolledAt time.Time `json:"enrolled_at"`
}

// Settings returns how to reach the central app the host is enrolled with.
func (e *Enrolment) Settings(version string) Settings {
	return Settings{URL: e.URL, CAFile: e.CAFile, AllowHTTP: e.AllowHTTP, Version: version, Credential: e.Credential}
}

// EnrolmentPath is the enrolment file belonging with the host key at
// keyPath.
func EnrolmentPath(keyPath string) string {
	return filepath.Join(filepath.Dir(keyPath), enrolmentFileName)
}

// LoadEnrolment reads the enrolment at path. It returns ErrNotEnrolled when
// there is none, and refuses a file that other users can read, since it
// holds the host's credential.
func LoadEnrolment(path string) (*Enrolment, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, fmt.Errorf("reading the enrolment: %w", err)
	}
	if err := checkPrivate(path, info); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the enrolment: %w", err)
	}
	var e Enrolment
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, fmt.Errorf("enrolment %s is damaged (remove it with `rest-o-matic unenrol` and enrol again): %w", path, err)
	}
	if e.URL == "" || e.HostID == "" || e.Credential == "" || e.ConfigPath == "" {
		return nil, fmt.Errorf("enrolment %s is incomplete (remove it with `rest-o-matic unenrol` and enrol again)", path)
	}
	return &e, nil
}

// SaveEnrolment writes e to path, readable only by its owner, replacing
// any enrolment there.
func SaveEnrolment(path string, e *Enrolment) error {
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("saving the enrolment: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".enrolment-*.json.tmp")
	if err != nil {
		return fmt.Errorf("saving the enrolment: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	err = tmp.Chmod(0o600)
	if err == nil {
		_, err = tmp.Write(append(data, '\n'))
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpPath, path)
	}
	if err != nil {
		return fmt.Errorf("saving the enrolment: %w", err)
	}
	return nil
}

// RemoveEnrolment deletes the enrolment at path. It returns ErrNotEnrolled
// when there is none.
func RemoveEnrolment(path string) error {
	err := os.Remove(path)
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotEnrolled
	}
	return err
}
