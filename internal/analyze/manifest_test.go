package analyze

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-birds/photosift/internal/ingest"
	_ "modernc.org/sqlite"
)

// TestDeleteManifestRespectsDecisions verifies the export contract: when the
// user has marked specific photos for deletion, only those flow through; when
// no explicit decisions exist, every non-keeper candidate does. The browser
// automation depends on this distinction.
func TestDeleteManifestRespectsDecisions(t *testing.T) {
	dir := t.TempDir()
	a := writePNG(t, dir, "a.png", checkerboard(128, 128, 8, 0))
	copyFile(t, a, filepath.Join(dir, "a_copy.png"))
	copyFile(t, a, filepath.Join(dir, "a_third.png"))

	dbPath := filepath.Join(dir, "t.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := ingest.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.ScanPaths(db, []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(db, Options{}); err != nil {
		t.Fatal(err)
	}

	// No decisions yet -> every non-keeper candidate is exported.
	manifest, err := DeleteManifest(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 2 {
		t.Fatalf("expected 2 candidates with no decisions, got %d", len(manifest))
	}

	// Mark one as delete; the manifest should switch to "only explicit deletes".
	target := manifest[0].ID
	if _, err := db.Exec(`INSERT INTO decisions(image_id, action, decided_at_unix) VALUES(?, 'delete', ?)`,
		target, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	manifest, err = DeleteManifest(db)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 1 || manifest[0].ID != target {
		t.Fatalf("expected only image %d after marking, got %+v", target, manifest)
	}
}
