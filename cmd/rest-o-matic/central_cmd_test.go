//go:build !windows

package main

import (
	"compress/gzip"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/central"
	"github.com/drewlsvern/rest-o-matic/internal/central/contracttest"
)

const (
	goodToken  = "one-time-token"
	credential = "rom1_test-credential"
)

// fakeCentral is a central app that records what hosts send it.
type fakeCentral struct {
	t   *testing.T
	srv *httptest.Server

	mu       sync.Mutex
	enrols   [][]byte
	checkins [][]byte
	// What it replies with: recovery keys offered at enrolment, parts asked
	// for again, and a status to fail check-ins with (0 for success).
	recovery []string
	resend   []string
	failWith int
	hang     chan struct{}
}

func newFakeCentral(t *testing.T, tlsServer bool) *fakeCentral {
	f := &fakeCentral{t: t}
	handler := http.HandlerFunc(f.serve)
	if tlsServer {
		f.srv = httptest.NewTLSServer(handler)
	} else {
		f.srv = httptest.NewServer(handler)
	}
	t.Cleanup(func() {
		f.mu.Lock()
		if f.hang != nil {
			close(f.hang)
			f.hang = nil
		}
		f.mu.Unlock()
		f.srv.Close()
	})
	return f
}

func (f *fakeCentral) serve(w http.ResponseWriter, r *http.Request) {
	var body io.Reader = r.Body
	if r.Header.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		body = zr
	}
	data, _ := io.ReadAll(body)
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}
	apiError := func(status int, code string) {
		reply(status, map[string]any{"error": map[string]string{"code": code, "message": "test says no"}})
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.URL.Path {
	case "/api/v1/enrol":
		f.enrols = append(f.enrols, data)
		var req central.EnrolRequest
		json.Unmarshal(data, &req)
		if req.Token != goodToken {
			apiError(401, "token_rejected")
			return
		}
		reply(200, central.EnrolResponse{FormatVersion: 1, HostID: "host-123", HostName: "test-host", Credential: credential, RecoveryRecipients: orNone(f.recovery)})
	case "/api/v1/checkin":
		if hang := f.hang; hang != nil {
			f.mu.Unlock()
			<-hang
			f.mu.Lock()
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+credential {
			apiError(401, "credential_rejected")
			return
		}
		if f.failWith != 0 {
			apiError(f.failWith, "unavailable")
			return
		}
		f.checkins = append(f.checkins, data)
		reply(200, map[string]any{"format_version": 1, "server_time": time.Now().UTC().Format(time.RFC3339), "resend": orNone(f.resend), "config": nil, "actions": []any{}})
		f.resend = nil
	default:
		http.NotFound(w, r)
	}
}

func orNone(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// caFile writes the server's certificate as a CA file, or returns "" for
// a plain http server.
func (f *fakeCentral) caFile() string {
	if f.srv.Certificate() == nil {
		return ""
	}
	path := filepath.Join(f.t.TempDir(), "ca.pem")
	data := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.srv.Certificate().Raw})
	if err := os.WriteFile(path, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
	return path
}

func (f *fakeCentral) set(change func(f *fakeCentral)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func (f *fakeCentral) checkinCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.checkins)
}

// lastCheckin returns the most recent check-in, after checking it against
// the contract.
func (f *fakeCentral) lastCheckin() central.CheckinRequest {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.checkins) == 0 {
		f.t.Fatal("no check-in was received")
	}
	data := f.checkins[len(f.checkins)-1]
	contracttest.Validate(f.t, "checkin-request", data)
	var req central.CheckinRequest
	if err := json.Unmarshal(data, &req); err != nil {
		f.t.Fatal(err)
	}
	return req
}

// centralHost is a workspace with a restic repository, a config and its
// own key directory.
type centralHost struct {
	t                    *testing.T
	bin, workdir, config string
	keyFile              string
}

func newCentralHost(t *testing.T) *centralHost {
	t.Helper()
	requireRestic(t)
	workdir, configPath := setupWorkspace(t)
	return &centralHost{t: t, bin: buildBinary(t), workdir: workdir, config: configPath, keyFile: filepath.Join(workdir, "keys", "host.key")}
}

func (h *centralHost) run(args ...string) (stdout, stderr string, code int) {
	h.t.Helper()
	return runCLI(h.t, h.bin, h.workdir, append([]string{"--config", h.config, "--key-file", h.keyFile}, args...)...)
}

func (h *centralHost) enrol(f *fakeCentral, extra ...string) {
	h.t.Helper()
	args := []string{"enrol", f.srv.URL, "--token", goodToken}
	if ca := f.caFile(); ca != "" {
		args = append(args, "--ca-file", ca)
	}
	args = append(args, extra...)
	if stdout, stderr, code := h.run(args...); code != 0 {
		h.t.Fatalf("enrol (exit %d): %s %s", code, stdout, stderr)
	}
}

func (h *centralHost) enrolmentPath() string {
	return filepath.Join(filepath.Dir(h.keyFile), "enrolment.json")
}

// lockPassword replaces the config's plain password with a locked one, so
// that the config can be sent.
func (h *centralHost) lockPassword() {
	h.t.Helper()
	cmd := []string{"--key-file", h.keyFile, "secret", "lock"}
	stdout, stderr, code := runCLIInput(h.t, h.bin, h.workdir, "testpass\n", cmd...)
	if code != 0 {
		h.t.Fatalf("lock (exit %d): %s", code, stderr)
	}
	data, _ := os.ReadFile(h.config)
	updated := strings.Replace(string(data), "password: testpass", "password: "+strings.TrimSpace(stdout), 1)
	if err := os.WriteFile(h.config, []byte(updated), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

func runCLIInput(t *testing.T, bin, workdir, input string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(input)
	var out, errOut outputBuf
	cmd.Stdout, cmd.Stderr = &out, &errOut
	code := 0
	if err := cmd.Run(); err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running CLI: %v", err)
		}
		code = exitErr.ExitCode()
	}
	return out.String(), errOut.String(), code
}

func included(req central.CheckinRequest) string {
	return strings.Join(req.Included(), ",")
}

func TestCLI_EnrolThenTickReportsOnlyChanges(t *testing.T) {
	h := newCentralHost(t)
	f := newFakeCentral(t, true)

	stdout, stderr, code := h.run("enrol", f.srv.URL, "--token", goodToken, "--ca-file", f.caFile())
	if code != 0 || !contains(stdout, "enrolled") || !contains(stdout, "test-host") || !contains(stdout, "created this host's key") {
		t.Fatalf("enrol (exit %d): %s %s", code, stdout, stderr)
	}
	contracttest.Validate(t, "enrol-request", f.enrols[0])
	info, err := os.Stat(h.enrolmentPath())
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("enrolment file: %v %v", err, info)
	}
	if _, err := os.Stat(h.keyFile); err != nil {
		t.Fatalf("no host key was created: %v", err)
	}
	var e central.Enrolment
	data, _ := os.ReadFile(h.enrolmentPath())
	json.Unmarshal(data, &e)
	if e.ConfigPath != h.config && !strings.HasSuffix(e.ConfigPath, "rest-o-matic.yaml") || e.HostID != "host-123" {
		t.Errorf("enrolment: %+v", e)
	}

	// The first tick sends everything there is to send. The password is
	// plain text, so the config is withheld and says why.
	if stdout, stderr, code := h.run("tick"); code != 0 {
		t.Fatalf("tick (exit %d): %s %s", code, stdout, stderr)
	} else if contains(stdout+stderr, "check-in") {
		t.Errorf("a successful check-in printed something: %s %s", stdout, stderr)
	}
	first := f.lastCheckin()
	if included(first) != "status,snapshots" || first.Parts.Config.Withheld == nil ||
		strings.Join(first.Parts.Config.Withheld.Fields, ",") != "repositories.nas.password" {
		t.Fatalf("first check-in: included %s, withheld %+v", included(first), first.Parts.Config.Withheld)
	}
	if *first.HostID != "host-123" || first.Host.ResticVersion == nil || first.Host.OS == "" {
		t.Errorf("host: %+v", first.Host)
	}

	// The check-in came before the job ran, so the next one reports the
	// run and its snapshots.
	h.run("tick")
	second := f.lastCheckin()
	if included(second) != "status,snapshots" {
		t.Fatalf("after the job ran: included %q", included(second))
	}
	var status statusPart
	json.Unmarshal(second.Parts.Status.Content, &status)
	if len(status.Jobs) != 1 || status.Jobs[0].LastRun == nil || len(status.Jobs[0].Runs) != 1 {
		t.Errorf("status part: %s", second.Parts.Status.Content)
	}
	var snaps snapshotsPart
	json.Unmarshal(second.Parts.Snapshots.Content, &snaps)
	if len(snaps.Jobs["documents"]["nas"].Snapshots) != 1 {
		t.Errorf("snapshots part: %s", second.Parts.Snapshots.Content)
	}

	// Nothing has happened since: only fingerprints.
	h.run("tick")
	if quiet := f.lastCheckin(); included(quiet) != "" || quiet.Parts.Status.Fingerprint != second.Parts.Status.Fingerprint {
		state, _ := os.ReadFile(filepath.Join(h.workdir, ".rest-o-matic", "state.json"))
		t.Fatalf("quiet check-in included %q\nsecond: %s\nquiet: %s\nstate: %s", included(quiet), second.Parts.Status.Content, quiet.Parts.Status.Content, state)
	}

	// Once the password is locked the config is sent, once.
	h.lockPassword()
	h.run("tick")
	if req := f.lastCheckin(); included(req) != "config" || req.Parts.Config.Withheld != nil || !contains(*req.Parts.Config.Content, "!locked") {
		t.Fatalf("after locking: included %q, withheld %+v", included(req), req.Parts.Config.Withheld)
	}
	h.run("tick")
	if req := f.lastCheckin(); included(req) != "" {
		t.Fatalf("config sent twice: %q", included(req))
	}

	// The central app asks for the snapshots again.
	f.set(func(f *fakeCentral) { f.resend = []string{"snapshots"} })
	h.run("tick")
	h.run("tick")
	if req := f.lastCheckin(); included(req) != "snapshots" {
		t.Fatalf("after a resend request: included %q", included(req))
	}
}

func TestCLI_CheckinFailuresWarnOnceAndResend(t *testing.T) {
	h := newCentralHost(t)
	f := newFakeCentral(t, true)
	h.enrol(f)
	h.run("tick")
	before := f.checkinCount()

	f.set(func(f *fakeCentral) { f.failWith = 503 })
	_, stderr, code := h.run("tick")
	if code != 0 || strings.Count(stderr, "warning:") != 1 || !contains(stderr, "503") {
		t.Fatalf("first failure (exit %d): %s", code, stderr)
	}
	for range 2 {
		if stdout, stderr, code := h.run("tick"); code != 0 || contains(stdout+stderr, "check-in") {
			t.Fatalf("later failure printed (exit %d): %s %s", code, stdout, stderr)
		}
	}
	if stdout, _, _ := h.run("status"); !contains(stdout, "failing:") || !contains(stdout, "503") {
		t.Errorf("status during the outage:\n%s", stdout)
	}

	f.set(func(f *fakeCentral) { f.failWith = 0 })
	stdout, stderr, _ := h.run("tick")
	if !contains(stdout, "working again") || contains(stderr, "warning") {
		t.Fatalf("recovery: %s %s", stdout, stderr)
	}
	if f.checkinCount() != before+1 {
		t.Fatalf("got %d check-ins, want 1 more", f.checkinCount()-before)
	}
	// The first tick's job ran meanwhile; nothing was acknowledged during
	// the outage, so the status is sent now.
	if req := f.lastCheckin(); !contains(included(req), "status") {
		t.Fatalf("after recovery: included %q", included(req))
	}
	if stdout, _, _ := h.run("tick"); contains(stdout, "working again") {
		t.Error("the recovery notice was printed twice")
	}
}

func TestCLI_CheckinCredentialRejected(t *testing.T) {
	h := newCentralHost(t)
	f := newFakeCentral(t, true)
	h.enrol(f)

	data, _ := os.ReadFile(h.enrolmentPath())
	os.WriteFile(h.enrolmentPath(), []byte(strings.Replace(string(data), credential, "rom1_deleted", 1)), 0o600)
	_, stderr, code := h.run("tick")
	if code != 0 || !contains(stderr, "rejected this host's credential") || !contains(stderr, "must be enrolled again") {
		t.Fatalf("tick (exit %d): %s", code, stderr)
	}
	if stdout, _, _ := h.run("status"); !contains(stdout, "must be enrolled again") {
		t.Errorf("status:\n%s", stdout)
	}
	if _, stderr, code := h.run("checkin"); code == 0 || !contains(stderr, "must be enrolled again") {
		t.Errorf("checkin (exit %d): %s", code, stderr)
	}
}

func TestCLI_UnreachableCentralDoesNotStopJobs(t *testing.T) {
	h := newCentralHost(t)
	f := newFakeCentral(t, true)
	h.enrol(f)
	// Everything is acknowledged, so the next check-in is a heartbeat,
	// with the short time limit.
	if _, stderr, code := h.run("checkin"); code != 0 {
		t.Fatalf("checkin: %s", stderr)
	}
	f.set(func(f *fakeCentral) { f.hang = make(chan struct{}) })

	start := time.Now()
	stdout, stderr, code := h.run("tick")
	took := time.Since(start)
	if code != 0 || !contains(stdout, "1 succeeded") {
		t.Fatalf("tick (exit %d): %s %s", code, stdout, stderr)
	}
	if !contains(stderr, "did not reply in time") {
		t.Errorf("expected a timeout warning, got: %s", stderr)
	}
	if took < heartbeatTimeout || took > heartbeatTimeout+10*time.Second {
		t.Errorf("tick took %v; the check-in should have given up after %v", took, heartbeatTimeout)
	}

	// Connection refused: no delay at all.
	f.set(func(f *fakeCentral) { close(f.hang); f.hang = nil })
	f.srv.Close()
	start = time.Now()
	if _, _, code := h.run("tick"); code != 0 || time.Since(start) > 5*time.Second {
		t.Errorf("tick with the server gone (exit %d) took %v", code, time.Since(start))
	}
}

func TestCLI_NoCheckinForOtherConfigOrAfterUnenrol(t *testing.T) {
	h := newCentralHost(t)
	f := newFakeCentral(t, true)
	h.enrol(f)

	other := filepath.Join(h.workdir, "other.yaml")
	data, _ := os.ReadFile(h.config)
	os.WriteFile(other, data, 0o644)
	if _, _, code := runCLI(t, h.bin, h.workdir, "--config", other, "--key-file", h.keyFile, "tick"); code != 0 {
		t.Fatal("tick with another config failed")
	}
	if f.checkinCount() != 0 {
		t.Fatal("a tick with another config checked in")
	}
	if stdout, _, _ := runCLI(t, h.bin, h.workdir, "--config", other, "--key-file", h.keyFile, "status"); !contains(stdout, "not connected") || !contains(stdout, "enrolled for") {
		t.Errorf("status for another config:\n%s", stdout)
	}

	stdout, _, code := h.run("unenrol")
	if code != 0 || !contains(stdout, "unenrolled from "+f.srv.URL) {
		t.Fatalf("unenrol (exit %d): %s", code, stdout)
	}
	if _, err := os.Stat(h.keyFile); err != nil {
		t.Error("unenrol removed the host key")
	}
	h.run("tick")
	if f.checkinCount() != 0 {
		t.Fatal("a tick after unenrol checked in")
	}
	if stdout, _, _ := h.run("status"); !contains(stdout, "central app: not connected") {
		t.Errorf("status after unenrol:\n%s", stdout)
	}
	if stdout, _, code := h.run("unenrol"); code != 0 || !contains(stdout, "not enrolled") {
		t.Errorf("second unenrol (exit %d): %s", code, stdout)
	}
}

func TestCLI_EnrolRefusals(t *testing.T) {
	h := newCentralHost(t)
	f := newFakeCentral(t, true)

	// A refused token stores nothing.
	_, stderr, code := h.run("enrol", f.srv.URL, "--token", "wrong", "--ca-file", f.caFile())
	if code == 0 || !contains(stderr, "rejected the enrolment token") {
		t.Fatalf("bad token (exit %d): %s", code, stderr)
	}
	if _, err := os.Stat(h.enrolmentPath()); err == nil {
		t.Fatal("an enrolment was stored for a refused token")
	}

	// An untrusted certificate.
	if _, stderr, code := h.run("enrol", f.srv.URL, "--token", goodToken); code == 0 || !contains(stderr, "certificate") {
		t.Fatalf("untrusted certificate (exit %d): %s", code, stderr)
	}

	// No token and nothing on standard input.
	if _, stderr, code := h.run("enrol", f.srv.URL, "--ca-file", f.caFile()); code == 0 || !contains(stderr, "no enrolment token") {
		t.Fatalf("no token (exit %d): %s", code, stderr)
	}

	// Plain http is refused before anything is sent, unless allowed.
	plain := newFakeCentral(t, false)
	if _, stderr, code := h.run("enrol", plain.srv.URL, "--token", goodToken); code == 0 || !contains(stderr, "--allow-http") {
		t.Fatalf("plain http (exit %d): %s", code, stderr)
	}
	if len(plain.enrols) != 0 {
		t.Fatal("plain http was contacted")
	}
	h.enrol(plain, "--allow-http")
	h.run("tick")
	if plain.checkinCount() != 1 {
		t.Fatal("a host enrolled over http did not check in")
	}

	// Already enrolled: refused without contacting anyone.
	enrolsBefore := len(f.enrols)
	if _, stderr, code := h.run("enrol", f.srv.URL, "--token", goodToken, "--ca-file", f.caFile()); code == 0 || !contains(stderr, "already enrolled with "+plain.srv.URL) {
		t.Fatalf("second enrol (exit %d): %s", code, stderr)
	}
	if len(f.enrols) != enrolsBefore {
		t.Fatal("a second enrol contacted the central app")
	}
	h.enrol(f, "--force")
	h.run("tick")
	if f.checkinCount() != 1 || plain.checkinCount() != 1 {
		t.Fatalf("after --force: %d / %d check-ins", f.checkinCount(), plain.checkinCount())
	}
}

func TestCLI_EnrolRecoveryKeys(t *testing.T) {
	h := newCentralHost(t)
	recovery := newSecretHost(t, h.bin)
	recoveryKey := recovery.keygen()
	f := newFakeCentral(t, true)
	f.recovery = []string{recoveryKey}
	recipients := filepath.Join(filepath.Dir(h.keyFile), "recovery-recipients")

	// Nobody to confirm it: not added.
	stdout, stderr, code := h.run("enrol", f.srv.URL, "--token", goodToken, "--ca-file", f.caFile())
	if code != 0 || !contains(stdout, recoveryKey) || !contains(stderr, "was not added") {
		t.Fatalf("enrol (exit %d): %s %s", code, stdout, stderr)
	}
	if listed, _ := listedRecipients(recipients); listed[recoveryKey] {
		t.Fatal("an unconfirmed recovery key was added")
	}

	// Named on the command line: added, and since the config now holds a
	// locked value, the command to re-lock it is printed.
	h.lockPassword()
	stdout, stderr, code = h.run("enrol", f.srv.URL, "--token", goodToken, "--ca-file", f.caFile(), "--force", "--accept-recovery-key", recoveryKey)
	if code != 0 || !contains(stdout, "added it to") || !contains(stdout, "secret relock") {
		t.Fatalf("enrol --accept-recovery-key (exit %d): %s %s", code, stdout, stderr)
	}
	if listed, _ := listedRecipients(recipients); !listed[recoveryKey] {
		t.Fatal("the named recovery key was not added")
	}
	if _, _, code := recovery.run("", "--config", h.config, "secret", "reveal", "nas"); code == 0 {
		t.Fatal("expected the recovery key not to open a value locked before it was added")
	}
	if _, stderr, code := h.run("secret", "relock"); code != 0 {
		t.Fatalf("relock: %s", stderr)
	}
	if got, _, code := recovery.run("", "--config", h.config, "secret", "reveal", "nas"); code != 0 || got != "testpass\n" {
		t.Fatalf("after relock the recovery key opened %q (exit %d)", got, code)
	}

	// Offered again: already listed, so not shown.
	if stdout, _, _ := h.run("enrol", f.srv.URL, "--token", goodToken, "--ca-file", f.caFile(), "--force"); contains(stdout, recoveryKey) {
		t.Errorf("an already listed key was offered again: %s", stdout)
	}
}

func TestCLI_CheckinCommand(t *testing.T) {
	h := newCentralHost(t)
	f := newFakeCentral(t, true)

	// --print works without enrolling, and sends nothing.
	stdout, stderr, code := h.run("checkin", "--print")
	if code != 0 {
		t.Fatalf("checkin --print (exit %d): %s", code, stderr)
	}
	contracttest.Validate(t, "checkin-request", []byte(stdout))
	var printed central.CheckinRequest
	json.Unmarshal([]byte(stdout), &printed)
	if printed.HostID != nil || included(printed) != "status,snapshots" || printed.Parts.Config.Withheld == nil {
		t.Errorf("printed: host %v, included %q", printed.HostID, included(printed))
	}
	if _, err := os.Stat(filepath.Join(h.workdir, ".rest-o-matic")); err == nil {
		t.Error("checkin --print created the state directory")
	}

	if _, stderr, code := h.run("checkin"); code == 0 || !contains(stderr, "not enrolled") {
		t.Fatalf("checkin before enrolling (exit %d): %s", code, stderr)
	}

	h.enrol(f)
	stdout, _, code = h.run("checkin")
	if code != 0 || !contains(stdout, "checked in with "+f.srv.URL+": sent status, snapshots") || !contains(stdout, "repositories.nas.password is in plain text") {
		t.Fatalf("checkin (exit %d): %s", code, stdout)
	}
	if stdout, _, _ := h.run("checkin"); !contains(stdout, "nothing had changed") {
		t.Fatalf("second checkin: %s", stdout)
	}
	if stdout, _, _ := h.run("checkin", "--print"); !contains(stdout, `"host_id": "host-123"`) {
		t.Errorf("printed check-in of an enrolled host has no host ID")
	}

	f.set(func(f *fakeCentral) { f.failWith = 500 })
	if _, stderr, code := h.run("checkin"); code == 0 || !contains(stderr, "500") {
		t.Fatalf("failing checkin (exit %d): %s", code, stderr)
	}
}

func TestCLI_StatusShowsCheckin(t *testing.T) {
	h := newCentralHost(t)
	f := newFakeCentral(t, true)

	// statusCheckin returns status --json's checkin object, after checking
	// that it has every key, null or not.
	statusCheckin := func() checkinStatus {
		t.Helper()
		stdout, stderr, code := h.run("status", "--json")
		if code != 0 {
			t.Fatalf("status --json (exit %d): %s", code, stderr)
		}
		var raw struct {
			Checkin json.RawMessage `json:"checkin"`
		}
		json.Unmarshal([]byte(stdout), &raw)
		keys := map[string]any{}
		json.Unmarshal(raw.Checkin, &keys)
		for _, key := range []string{"enrolled", "url", "host_id", "host_name", "last_attempt", "last_success", "last_error", "config_withheld"} {
			if _, ok := keys[key]; !ok {
				t.Errorf("status --json has no checkin.%s: %s", key, raw.Checkin)
			}
		}
		var c checkinStatus
		json.Unmarshal(raw.Checkin, &c)
		return c
	}

	if c := statusCheckin(); c.Enrolled || c.URL != nil {
		t.Errorf("not enrolled: %+v", c)
	}
	if stdout, _, _ := h.run("status"); !contains(stdout, "central app: not connected\n") {
		t.Errorf("status before enrolling:\n%s", stdout)
	}

	h.enrol(f)
	if stdout, _, _ := h.run("status"); !contains(stdout, "central app: "+f.srv.URL+", no check-in yet") || !contains(stdout, "config not backed up to the central app: repositories.nas.password is in plain text") {
		t.Errorf("status after enrolling:\n%s", stdout)
	}
	h.run("tick")
	c := statusCheckin()
	if !c.Enrolled || *c.HostName != "test-host" || c.LastSuccess == nil || c.LastError != nil || len(c.ConfigWithheld) != 1 {
		t.Errorf("after a check-in: %+v", c)
	}
	if stdout, _, _ := h.run("status"); !contains(stdout, "last check-in just now") {
		t.Errorf("status after a check-in:\n%s", stdout)
	}
	h.lockPassword()
	if c := statusCheckin(); c.ConfigWithheld != nil {
		t.Errorf("config still withheld after locking: %v", c.ConfigWithheld)
	}
}
