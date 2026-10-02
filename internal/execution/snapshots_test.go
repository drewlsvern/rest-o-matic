package execution

import (
	"testing"
	"time"
)

// What `restic forget --tag documents --keep-last 2 --prune --json` prints
// on restic 0.19: one line, snapshots carrying a summary.
const forgetOutput019 = `[{"tags":null,"host":"g5","paths":["/home/me/documents"],"keep":[{"time":"2026-10-02T01:31:00.0292019-05:00","parent":"aaa","tree":"t1","paths":["/home/me/documents"],"hostname":"g5","username":"me","uid":1000,"gid":1000,"tags":["documents"],"program_version":"restic 0.19.1","summary":{"backup_start":"2026-10-02T01:31:00-05:00","backup_end":"2026-10-02T01:31:01-05:00","files_new":0,"files_changed":1,"files_unmodified":0,"dirs_new":0,"dirs_changed":3,"dirs_unmodified":0,"data_blobs":1,"tree_blobs":4,"data_added":3142,"data_added_packed":1500,"total_files_processed":4212,"total_bytes_processed":1288490188},"id":"e974615e00000000000000000000000000000000000000000000000000000000","short_id":"e974615e"},{"time":"2026-10-02T01:30:59-05:00","tree":"t0","paths":["/home/me/documents"],"hostname":"g5","tags":["documents"],"summary":{"total_files_processed":4211,"total_bytes_processed":1288490000},"id":"0a3ca2ce00000000000000000000000000000000000000000000000000000000","short_id":"0a3ca2ce"}],"remove":[{"time":"2026-10-02T01:30:58-05:00","id":"dead","short_id":"dead"}],"reasons":[]}]
`

// The same on restic 0.16: no summary on a snapshot, and prune's progress
// follows as plain text, some of it starting with "[".
const forgetOutput016 = `[{"tags":null,"host":"g5","paths":["/home/me/documents"],"keep":[{"time":"2026-10-02T01:30:59-05:00","tree":"t0","paths":["/home/me/documents"],"hostname":"g5","username":"me","tags":["documents"],"program_version":"restic 0.16.4","id":"0a3ca2ce00000000000000000000000000000000000000000000000000000000","short_id":"0a3ca2ce"}],"remove":null,"reasons":[]}]
loading indexes...
loading all snapshots...
finding data that is still in use for 1 snapshots
[0:00] 100.00%  1 / 1 snapshots
searching used packs...
[0:00]          0 packs processed
done
`

func TestKeptSnapshots_Restic019(t *testing.T) {
	snaps, ok := keptSnapshots([]byte(forgetOutput019))
	if !ok || len(snaps) != 2 {
		t.Fatalf("got %d snapshots, ok=%v; want the two kept", len(snaps), ok)
	}
	newest := snaps[0]
	if newest.ShortID != "e974615e" || newest.Hostname != "g5" || newest.Paths[0] != "/home/me/documents" || newest.Tags[0] != "documents" {
		t.Errorf("got newest %+v, want the later snapshot with its host, paths and tags", newest)
	}
	want := time.Date(2026, 10, 2, 6, 31, 0, 29201900, time.UTC)
	if !newest.Time.Equal(want) {
		t.Errorf("got time %v, want %v", newest.Time, want)
	}
	if newest.Size == nil || *newest.Size != 1288490188 || newest.Files == nil || *newest.Files != 4212 {
		t.Errorf("got size=%v files=%v, want restic's summary totals", newest.Size, newest.Files)
	}
	if newest.FilesNew == nil || *newest.FilesNew != 0 || *newest.FilesChanged != 1 || *newest.DataAdded != 3142 || newest.DataAddedPacked == nil || *newest.DataAddedPacked != 1500 {
		t.Errorf("got new=%v changed=%v added=%v packed=%v, want what the snapshot changed", newest.FilesNew, newest.FilesChanged, newest.DataAdded, newest.DataAddedPacked)
	}
	// A summary without the packed figure leaves just that one unknown.
	if older := snaps[1]; older.ShortID != "0a3ca2ce" || older.DataAdded == nil || older.DataAddedPacked != nil {
		t.Errorf("got second %+v, want the older snapshot, with no packed size", older)
	}
}

func TestKeptSnapshots_Restic016HasNoSizes(t *testing.T) {
	snaps, ok := keptSnapshots([]byte(forgetOutput016))
	if !ok || len(snaps) != 1 {
		t.Fatalf("got %d snapshots, ok=%v; want the one kept, ignoring prune's text", len(snaps), ok)
	}
	if s := snaps[0]; s.ShortID != "0a3ca2ce" || s.Size != nil || s.Files != nil || s.FilesNew != nil || s.FilesChanged != nil || s.DataAdded != nil {
		t.Errorf("got %+v, want the snapshot with its size and what it changed unknown", s)
	}
}

// restic groups by host and paths, so a job whose paths changed has its
// snapshots spread over groups. They are one list, newest first.
func TestKeptSnapshots_SeveralGroupsAreOneList(t *testing.T) {
	out := `[{"host":"a","keep":[{"time":"2026-01-01T00:00:00Z","id":"old","short_id":"old"}]},` +
		`{"host":"b","keep":[{"time":"2026-01-03T00:00:00Z","id":"new","short_id":"new"},{"time":"2026-01-02T00:00:00Z","id":"mid","short_id":"mid"}]}]`
	snaps, ok := keptSnapshots([]byte(out))
	if !ok || len(snaps) != 3 {
		t.Fatalf("got %d snapshots, ok=%v; want all three", len(snaps), ok)
	}
	if got := snaps[0].ID + " " + snaps[1].ID + " " + snaps[2].ID; got != "new mid old" {
		t.Errorf("got order %q, want newest first across groups", got)
	}
}

func TestKeptSnapshots_EmptyListIsStillAList(t *testing.T) {
	for _, out := range []string{"[]", `[{"host":"a","keep":null,"remove":[{"id":"x"}]}]`} {
		snaps, ok := keptSnapshots([]byte(out))
		if !ok || snaps == nil || len(snaps) != 0 {
			t.Errorf("for %s got %v, ok=%v; want an empty list that counts as listed", out, snaps, ok)
		}
	}
}

func TestKeptSnapshots_NoListInOutput(t *testing.T) {
	for _, out := range []string{"", "no list here\n", "[0:00] 100.00%  1 / 1 snapshots\n", `{"message_type":"summary"}`} {
		if snaps, ok := keptSnapshots([]byte(out)); ok {
			t.Errorf("for %q got %v, ok=true; want no list", out, snaps)
		}
	}
}
