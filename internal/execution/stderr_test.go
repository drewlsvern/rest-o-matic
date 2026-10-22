package execution

import (
	"strings"
	"testing"
)

func TestStderrOutput_PicksOutRetries(t *testing.T) {
	var retries []string
	o := newStderrOutput(func(m string) { retries = append(retries, m) })
	text := strings.Join([]string{
		`Stat(<config/>) returned error, retrying after 1.130616019s: Stat: Get "https://192.0.2.1/bucket/?location=": dial tcp 192.0.2.1:443: i/o timeout`,
		`Load(<lock/1c2d3e>, 0, 0) returned error, retrying after 1m13.755647154s: unexpected EOF`,
		`List(data) returned error, retrying after 520.023193ms: connection reset by peer`,
		`{"message_type":"error","error":{"message":"read /x: permission denied"},"during":"archival","item":"/x"}`,
		`Fatal: unable to open config file: Stat: Get "https://192.0.2.1/bucket/?location=": dial tcp 192.0.2.1:443: i/o timeout`,
		`signal terminated received, cleaning up`,
	}, "\n") + "\n"
	// Written in pieces that split lines, as a pipe may.
	for i := 0; i < len(text); i += 37 {
		end := min(i+37, len(text))
		if _, err := o.Write([]byte(text[i:end])); err != nil {
			t.Fatal(err)
		}
	}
	o.close()

	want := []string{
		`Stat: Get "https://192.0.2.1/bucket/?location=": dial tcp 192.0.2.1:443: i/o timeout`,
		`unexpected EOF`,
		`connection reset by peer`,
	}
	if strings.Join(retries, "|") != strings.Join(want, "|") {
		t.Fatalf("got retries %q", retries)
	}
	if o.lastRetry != want[2] {
		t.Errorf("last retry %q", o.lastRetry)
	}
	// Everything is kept for error reporting, exactly as written.
	if o.String() != text {
		t.Errorf("stderr text changed: %q", o.String())
	}
	// Retries are left out of the error reported, so restic's real errors
	// show.
	want2 := `read /x: permission denied; unable to open config file: Stat: Get "https://192.0.2.1/bucket/?location=": dial tcp 192.0.2.1:443: i/o timeout`
	if got := resticMessages(o.String()); got != want2 {
		t.Errorf("restic's messages: %q\nwant %q", got, want2)
	}
}
