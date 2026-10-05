// Package central is the host's side of the link to the central app: the
// messages defined in contract/checkin/v1, the HTTP client that sends
// them, and the enrolment stored beside the host key.
package central

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// FormatVersion is the contract version these messages follow.
const FormatVersion = 1

// The three parts of a check-in.
const (
	PartStatus    = "status"
	PartSnapshots = "snapshots"
	PartConfig    = "config"
)

// Parts lists every part, in the order they are reported.
var Parts = []string{PartStatus, PartSnapshots, PartConfig}

// Host describes the machine a message comes from.
type Host struct {
	Hostname          string  `json:"hostname"`
	OS                string  `json:"os"`
	Arch              string  `json:"arch"`
	RestOMaticVersion string  `json:"rest_o_matic_version"`
	ResticVersion     *string `json:"restic_version"`
}

// EnrolRequest is sent once, to POST /api/v1/enrol.
type EnrolRequest struct {
	FormatVersion int    `json:"format_version"`
	Token         string `json:"token"`
	PublicKey     string `json:"public_key"`
	Host          Host   `json:"host"`
}

// EnrolResponse is the central app's reply to an enrolment.
type EnrolResponse struct {
	FormatVersion      int      `json:"format_version"`
	HostID             string   `json:"host_id"`
	HostName           string   `json:"host_name"`
	Credential         string   `json:"credential"`
	RecoveryRecipients []string `json:"recovery_recipients"`
}

// CheckinRequest is sent to POST /api/v1/checkin.
type CheckinRequest struct {
	FormatVersion int          `json:"format_version"`
	HostID        *string      `json:"host_id"`
	SentAt        time.Time    `json:"sent_at"`
	Host          Host         `json:"host"`
	Parts         CheckinParts `json:"parts"`
}

// CheckinParts holds each part's fingerprint, and its content when it is
// included.
type CheckinParts struct {
	Status    Part       `json:"status"`
	Snapshots Part       `json:"snapshots"`
	Config    ConfigPart `json:"config"`
}

// Part is the status or snapshots part. Content is null unless included.
type Part struct {
	Fingerprint string          `json:"fingerprint"`
	Content     json.RawMessage `json:"content"`
}

// included reports whether the part carries content. A request read back
// from JSON holds a literal null rather than nothing.
func (p Part) included() bool {
	return len(p.Content) > 0 && string(p.Content) != "null"
}

// ConfigPart is the config file. Content is its text, null unless
// included; Withheld says why it is never included while set.
type ConfigPart struct {
	Fingerprint string    `json:"fingerprint"`
	Content     *string   `json:"content"`
	Withheld    *Withheld `json:"withheld"`
}

// Withheld explains a config that is not sent.
type Withheld struct {
	Reason string   `json:"reason"`
	Fields []string `json:"fields"`
}

// WithheldPlainTextSecrets is the reason for a config holding plain-text
// secrets.
const WithheldPlainTextSecrets = "plain_text_secrets"

// Included lists the parts whose content the request carries.
func (r CheckinRequest) Included() []string {
	var parts []string
	if r.Parts.Status.included() {
		parts = append(parts, PartStatus)
	}
	if r.Parts.Snapshots.included() {
		parts = append(parts, PartSnapshots)
	}
	if r.Parts.Config.Content != nil {
		parts = append(parts, PartConfig)
	}
	return parts
}

// Fingerprints returns each part's fingerprint by name.
func (r CheckinRequest) Fingerprints() map[string]string {
	return map[string]string{
		PartStatus:    r.Parts.Status.Fingerprint,
		PartSnapshots: r.Parts.Snapshots.Fingerprint,
		PartConfig:    r.Parts.Config.Fingerprint,
	}
}

// CheckinResponse is the central app's reply to a check-in.
type CheckinResponse struct {
	FormatVersion int       `json:"format_version"`
	ServerTime    time.Time `json:"server_time"`
	// Resend lists parts to include in full next time.
	Resend []string `json:"resend"`
}

// Fingerprint is the fingerprint of data: "sha256:" and its hex SHA-256.
func Fingerprint(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
