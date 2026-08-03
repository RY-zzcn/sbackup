package backup

import (
	"encoding/json"
	"testing"
)

func TestResticSummaryJSONTags(t *testing.T) {
	var s Summary
	input := `{"message_type":"summary","snapshot_id":"snap","files_new":2,"files_changed":3,"files_unmodified":4,"data_added":5,"total_bytes_processed":6}`
	if err := json.Unmarshal([]byte(input), &s); err != nil {
		t.Fatal(err)
	}
	if s.SnapshotID != "snap" || s.FilesChanged != 3 || s.TotalBytesProcessed != 6 {
		t.Fatalf("bad summary: %#v", s)
	}
}
