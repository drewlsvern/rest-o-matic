package central

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Error codes the central app may reply with that the host acts on.
const (
	CodeCredentialRejected = "credential_rejected"
	CodeTokenRejected      = "token_rejected"
	CodeUnsupportedFormat  = "unsupported_format"
)

// gzipAbove is the request body size above which it is compressed.
const gzipAbove = 32 << 10

// maxReplySize bounds how much of a reply is read.
const maxReplySize = 4 << 20

// Errors a caller may need to tell apart, matched with errors.Is.
var (
	// ErrCredentialRejected means the central app no longer knows this
	// host's credential; the host must be enrolled again.
	ErrCredentialRejected = errors.New("the central app rejected this host's credential")
	// ErrTokenRejected means the enrolment token is unknown, used or
	// expired.
	ErrTokenRejected = errors.New("the central app rejected the enrolment token")
	// ErrUnsupportedFormat means the central app doesn't accept this
	// version of the messages.
	ErrUnsupportedFormat = errors.New("the central app does not accept this rest-o-matic's message format")
)

// APIError is an error reply from the central app.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	var known error
	switch e.Code {
	case CodeCredentialRejected:
		known = ErrCredentialRejected
	case CodeTokenRejected:
		known = ErrTokenRejected
	case CodeUnsupportedFormat:
		known = ErrUnsupportedFormat
	}
	text := fmt.Sprintf("the central app replied %d %s", e.Status, http.StatusText(e.Status))
	if known != nil {
		text = known.Error()
	}
	if e.Message != "" {
		text += ": " + e.Message
	}
	return text
}

// Is lets errors.Is match an APIError against the sentinel for its code.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrCredentialRejected:
		return e.Code == CodeCredentialRejected
	case ErrTokenRejected:
		return e.Code == CodeTokenRejected
	case ErrUnsupportedFormat:
		return e.Code == CodeUnsupportedFormat
	}
	return false
}

// Client sends messages to one central app.
type Client struct {
	baseURL    string
	http       *http.Client
	userAgent  string
	credential string
}

// Settings say how to reach a central app.
type Settings struct {
	URL string
	// CAFile, if set, names a PEM file of certificate authorities trusted
	// in addition to the system's.
	CAFile string
	// AllowHTTP permits a plain http:// URL.
	AllowHTTP bool
	// Version is rest-o-matic's version, for the User-Agent.
	Version string
	// Credential is sent as a bearer credential, when set.
	Credential string
}

// CheckURL returns the central app's address in the form it is stored:
// without a trailing slash, and refused unless it is https (or http when
// allowHTTP is set).
func CheckURL(raw string, allowHTTP bool) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("%q is not a web address such as https://backups.example.com", raw)
	}
	switch {
	case u.Scheme == "https":
	case u.Scheme == "http" && allowHTTP:
	case u.Scheme == "http":
		return "", fmt.Errorf("%s uses plain http, which would send the credential unencrypted; use https, or --allow-http for testing", raw)
	default:
		return "", fmt.Errorf("%q is not an https:// address", raw)
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("%q should be just the central app's address, such as https://backups.example.com", raw)
	}
	return strings.TrimRight(u.String(), "/"), nil
}

// NewClient returns a client for the central app described by s.
func NewClient(s Settings) (*Client, error) {
	base, err := CheckURL(s.URL, s.AllowHTTP)
	if err != nil {
		return nil, err
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if s.CAFile != "" {
		pem, err := os.ReadFile(s.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading the CA file: %w", err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%s holds no PEM certificate", s.CAFile)
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: pool}
	return &Client{
		baseURL: base,
		http: &http.Client{
			Transport: transport,
			// A redirect could take the credential somewhere else, or to
			// plain http; neither is followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		userAgent:  "rest-o-matic/" + s.Version,
		credential: s.Credential,
	}, nil
}

// Enrol registers the host.
func (c *Client) Enrol(ctx context.Context, req EnrolRequest) (EnrolResponse, error) {
	var resp EnrolResponse
	if err := c.post(ctx, "/api/v1/enrol", req, &resp); err != nil {
		return EnrolResponse{}, err
	}
	if resp.HostID == "" || resp.Credential == "" {
		return EnrolResponse{}, errors.New("the central app's enrolment reply has no host ID or credential")
	}
	return resp, nil
}

// Checkin sends one check-in.
func (c *Client) Checkin(ctx context.Context, req CheckinRequest) (CheckinResponse, error) {
	var resp CheckinResponse
	err := c.post(ctx, "/api/v1/checkin", req, &resp)
	return resp, err
}

func (c *Client) post(ctx context.Context, path string, body, reply any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	encoding := ""
	if len(data) > gzipAbove {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		zw.Write(data)
		if err := zw.Close(); err != nil {
			return err
		}
		data, encoding = buf.Bytes(), "gzip"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	if c.credential != "" {
		req.Header.Set("Authorization", "Bearer "+c.credential)
	}

	res, err := c.http.Do(req)
	if err != nil {
		return transportError(ctx, err)
	}
	defer res.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(res.Body, maxReplySize))
	if err != nil {
		return transportError(ctx, err)
	}

	if res.StatusCode != http.StatusOK {
		apiErr := &APIError{Status: res.StatusCode}
		var e struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(payload, &e) == nil {
			apiErr.Code, apiErr.Message = e.Error.Code, e.Error.Message
		}
		if loc := res.Header.Get("Location"); apiErr.Code == "" && loc != "" {
			apiErr.Message = "redirected to " + loc + ", which is not followed; check the address"
		}
		return apiErr
	}
	if err := json.Unmarshal(payload, reply); err != nil {
		return fmt.Errorf("the central app's reply is not what rest-o-matic expects: %w", err)
	}
	return nil
}

// transportError explains a request that got no reply.
func transportError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("the central app did not reply in time")
	}
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return fmt.Errorf("the central app's certificate could not be verified (if it is issued by a private authority, pass that authority with --ca-file): %w", certErr.Err)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}
