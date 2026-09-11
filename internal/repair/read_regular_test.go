package repair

import (
	"encoding/json"
	"io"
	"os"
	"testing"
)

func TestPendingUpdateReadHandlePreservesSnapshotAcrossArchive(t *testing.T) {
	fixture := prepareSupersededUpdateFixture(t, "v1.19.1", "v1.20.0")
	// Hold the same regular-file handle used by pending transaction readers
	// throughout a real archival. On Windows this requires delete sharing;
	// readers keep their old file identity while the path is atomically moved.
	reader, err := openRepairRegularRead(PendingUpdatePath())
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	archived, err := ArchiveSupersededPendingFileUpdate(fixture.running, fixture.root)
	if err != nil || !archived {
		t.Fatalf("archive with active reader: archived=%v err=%v", archived, err)
	}
	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot UpdateTransaction
	if err := json.Unmarshal(body, &snapshot); err != nil {
		t.Fatal(err)
	}
	if UpdateTransactionID(&snapshot) != UpdateTransactionID(fixture.transaction) {
		t.Fatal("reader lost the original transaction snapshot after archival")
	}
	if _, err := readPendingUpdateUnchecked(); !os.IsNotExist(err) {
		t.Fatalf("new reader observed an archived pending marker: %v", err)
	}
}
