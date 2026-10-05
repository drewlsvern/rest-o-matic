package state

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordCheckin(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(filepath.Join(dir, "state.json"), filepath.Join(dir, "locks"))
	t0 := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

	wasFailing, err := store.RecordCheckin("h1", t0, map[string]string{"status": "sha256:a", "config": "sha256:c"}, []string{"snapshots"}, nil)
	if err != nil || wasFailing {
		t.Fatalf("first success: %v %v", wasFailing, err)
	}

	// A failure records the error and keeps what was acknowledged.
	t1 := t0.Add(time.Minute)
	if wasFailing, _ = store.RecordCheckin("h1", t1, map[string]string{"status": "sha256:b"}, nil, errors.New("connection refused")); wasFailing {
		t.Error("the first failure said the one before had failed")
	}
	if wasFailing, _ = store.RecordCheckin("h1", t1, nil, nil, errors.New("connection refused")); !wasFailing {
		t.Error("the second failure did not see the first")
	}
	st, _ := store.Load()
	c := st.CheckinFor("h1")
	if c.LastError != "connection refused" || !c.LastAttempt.Equal(t1) || !c.LastSuccess.Equal(t0) {
		t.Errorf("after failures: %+v", c)
	}
	if c.Acknowledged["status"] != "sha256:a" || len(c.Resend) != 1 {
		t.Errorf("a failure changed what was acknowledged: %+v", c)
	}

	// Success again merges acknowledgements and replaces the resend list.
	t2 := t1.Add(time.Minute)
	if wasFailing, _ = store.RecordCheckin("h1", t2, map[string]string{"status": "sha256:b", "snapshots": "sha256:s"}, nil, nil); !wasFailing {
		t.Error("recovery did not see the failure")
	}
	st, _ = store.Load()
	c = st.CheckinFor("h1")
	if c.LastError != "" || c.Acknowledged["status"] != "sha256:b" || c.Acknowledged["config"] != "sha256:c" || c.Acknowledged["snapshots"] != "sha256:s" || len(c.Resend) != 0 {
		t.Errorf("after recovery: %+v", c)
	}

	// Another enrolment starts afresh.
	if c := st.CheckinFor("h2"); c.HostID != "h2" || c.LastAttempt != nil || len(c.Acknowledged) != 0 {
		t.Errorf("a new enrolment inherited %+v", c)
	}
	if _, err := store.RecordCheckin("h2", t2, nil, nil, errors.New("x")); err != nil {
		t.Fatal(err)
	}
	st, _ = store.Load()
	if c := st.CheckinFor("h2"); c.Acknowledged["status"] != "" {
		t.Errorf("a new enrolment kept the old acknowledgements: %+v", c)
	}
}
