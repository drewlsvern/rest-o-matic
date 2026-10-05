package central

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// caFile writes the test server's certificate as a PEM file, so the
// client can trust it as an authority.
func caFile(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func checkin() CheckinRequest {
	id := "host-1"
	return CheckinRequest{FormatVersion: FormatVersion, HostID: &id, SentAt: time.Now().UTC(),
		Parts: CheckinParts{Status: Part{Fingerprint: Fingerprint(nil)}, Snapshots: Part{Fingerprint: Fingerprint(nil)}, Config: ConfigPart{Fingerprint: Fingerprint(nil)}}}
}

func TestClient_SendsCheckinWithCredential(t *testing.T) {
	var got *http.Request
	var body []byte
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		var reader io.Reader = r.Body
		if r.Header.Get("Content-Encoding") == "gzip" {
			zr, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Errorf("gzip: %v", err)
				return
			}
			reader = zr
		}
		body, _ = io.ReadAll(reader)
		w.Write([]byte(`{"format_version":1,"server_time":"2026-10-03T09:00:00Z","resend":["snapshots"],"something_new":true}`))
	}))
	defer srv.Close()

	c, err := NewClient(Settings{URL: srv.URL + "/", CAFile: caFile(t, srv), Version: "v1.2.3", Credential: "rom1_secret"})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := c.Checkin(context.Background(), checkin())
	if err != nil {
		t.Fatalf("Checkin: %v", err)
	}
	if got.URL.Path != "/api/v1/checkin" || got.Method != http.MethodPost {
		t.Errorf("got %s %s", got.Method, got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer rom1_secret" || got.Header.Get("User-Agent") != "rest-o-matic/v1.2.3" || got.Header.Get("Content-Type") != "application/json" {
		t.Errorf("headers: %v", got.Header)
	}
	if got.Header.Get("Content-Encoding") != "" {
		t.Error("a small check-in was compressed")
	}
	var sent CheckinRequest
	if err := json.Unmarshal(body, &sent); err != nil || *sent.HostID != "host-1" {
		t.Errorf("body: %s (%v)", body, err)
	}
	if len(reply.Resend) != 1 || reply.Resend[0] != PartSnapshots {
		t.Errorf("reply: %+v", reply)
	}

	// A large body is compressed.
	big := checkin()
	big.Parts.Status.Content = json.RawMessage(`"` + strings.Repeat("x", 40<<10) + `"`)
	if _, err := c.Checkin(context.Background(), big); err != nil {
		t.Fatal(err)
	}
	if got.Header.Get("Content-Encoding") != "gzip" || !bytes.Contains(body, []byte("xxxx")) {
		t.Errorf("a large check-in was not sent gzip-compressed (encoding %q)", got.Header.Get("Content-Encoding"))
	}
}

func TestClient_ErrorReplies(t *testing.T) {
	cases := []struct {
		status   int
		body     string
		sentinel error
		text     string
	}{
		{401, `{"error":{"code":"credential_rejected","message":"Unknown host."}}`, ErrCredentialRejected, "rejected this host's credential: Unknown host."},
		{401, `{"error":{"code":"token_rejected","message":"Token expired."}}`, ErrTokenRejected, "rejected the enrolment token: Token expired."},
		{400, `{"error":{"code":"unsupported_format","message":"Wants 2."}}`, ErrUnsupportedFormat, "message format: Wants 2."},
		{502, `<html>Bad gateway</html>`, nil, "replied 502 Bad Gateway"},
	}
	for _, c := range cases {
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
			w.Write([]byte(c.body))
		}))
		client, _ := NewClient(Settings{URL: srv.URL, CAFile: caFile(t, srv)})
		_, err := client.Checkin(context.Background(), checkin())
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), c.text) {
			t.Errorf("%d: got %v, want it to contain %q", c.status, err, c.text)
		}
		if c.sentinel != nil && !errors.Is(err, c.sentinel) {
			t.Errorf("%d: %v is not %v", c.status, err, c.sentinel)
		}
		if c.sentinel == nil && (errors.Is(err, ErrCredentialRejected) || errors.Is(err, ErrTokenRejected)) {
			t.Errorf("%d: %v matched a specific error", c.status, err)
		}
	}
}

func TestClient_UntrustedCertificate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c, _ := NewClient(Settings{URL: srv.URL})
	_, err := c.Checkin(context.Background(), checkin())
	if err == nil || !strings.Contains(err.Error(), "certificate could not be verified") || !strings.Contains(err.Error(), "--ca-file") {
		t.Fatalf("got %v, want a certificate error mentioning --ca-file", err)
	}
}

func TestClient_RedirectNotFollowed(t *testing.T) {
	hit := false
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit = true }))
	defer target.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/v1/checkin", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	c, _ := NewClient(Settings{URL: srv.URL, CAFile: caFile(t, srv), Credential: "x"})
	_, err := c.Checkin(context.Background(), checkin())
	if err == nil || !strings.Contains(err.Error(), "not followed") || hit {
		t.Fatalf("got %v (followed: %v), want the redirect refused", err, hit)
	}
}

func TestClient_TimeLimit(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)
	c, _ := NewClient(Settings{URL: srv.URL, CAFile: caFile(t, srv)})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Checkin(ctx, checkin())
	if err == nil || !strings.Contains(err.Error(), "did not reply in time") || time.Since(start) > 2*time.Second {
		t.Fatalf("got %v after %v, want a timeout", err, time.Since(start))
	}
}

func TestCheckURL(t *testing.T) {
	for raw, want := range map[string]string{
		"https://backups.example.com/":    "https://backups.example.com",
		"https://backups.example.com/rom": "https://backups.example.com/rom",
		" https://h.tailnet.ts.net:8443 ": "https://h.tailnet.ts.net:8443",
	} {
		if got, err := CheckURL(raw, false); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{"backups.example.com", "ftp://x", "https://", "https://u:p@x", "https://x/?a=b"} {
		if _, err := CheckURL(raw, true); err == nil {
			t.Errorf("%q was accepted", raw)
		}
	}
	if _, err := CheckURL("http://x", false); err == nil || !strings.Contains(err.Error(), "--allow-http") {
		t.Errorf("plain http without --allow-http: %v", err)
	}
	if got, err := CheckURL("http://x", true); err != nil || got != "http://x" {
		t.Errorf("plain http with --allow-http: %q %v", got, err)
	}
}
