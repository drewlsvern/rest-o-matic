package execution

import (
	"bytes"
	"encoding/json"
	"sort"
	"time"

	"github.com/drewlsvern/rest-o-matic/internal/state"
)

// forgetGroup is one group in `restic forget --json` output. restic groups
// snapshots by host and paths, and reports for each group which it kept.
type forgetGroup struct {
	Keep []struct {
		ID       string    `json:"id"`
		ShortID  string    `json:"short_id"`
		Time     time.Time `json:"time"`
		Hostname string    `json:"hostname"`
		Paths    []string  `json:"paths"`
		Tags     []string  `json:"tags"`
		// Summary is absent before restic 0.17.
		Summary *struct {
			TotalBytesProcessed int64  `json:"total_bytes_processed"`
			TotalFilesProcessed int64  `json:"total_files_processed"`
			FilesNew            int64  `json:"files_new"`
			FilesChanged        int64  `json:"files_changed"`
			DataAdded           int64  `json:"data_added"`
			DataAddedPacked     *int64 `json:"data_added_packed"`
		} `json:"summary"`
	} `json:"keep"`
}

// keptSnapshots reads the snapshots a `restic forget --json` run kept, from
// its standard output: every group's kept snapshots, newest first. ok is
// false when the output holds no list at all.
//
// The list is the first line that is a JSON array. Older restic follows it
// with prune's plain-text progress, some of which also starts with "[", so
// a line only counts if it decodes.
func keptSnapshots(output []byte) (snapshots []state.Snapshot, ok bool) {
	for _, line := range bytes.Split(output, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("[")) {
			continue
		}
		var groups []forgetGroup
		if err := json.Unmarshal(line, &groups); err != nil {
			continue
		}
		snapshots = []state.Snapshot{}
		for _, g := range groups {
			for _, k := range g.Keep {
				s := state.Snapshot{ID: k.ID, ShortID: k.ShortID, Time: k.Time, Hostname: k.Hostname, Paths: k.Paths, Tags: k.Tags}
				if sum := k.Summary; sum != nil {
					s.Size, s.Files = &sum.TotalBytesProcessed, &sum.TotalFilesProcessed
					s.FilesNew, s.FilesChanged = &sum.FilesNew, &sum.FilesChanged
					s.DataAdded, s.DataAddedPacked = &sum.DataAdded, sum.DataAddedPacked
				}
				snapshots = append(snapshots, s)
			}
		}
		sort.SliceStable(snapshots, func(i, j int) bool { return snapshots[i].Time.After(snapshots[j].Time) })
		return snapshots, true
	}
	return nil, false
}
