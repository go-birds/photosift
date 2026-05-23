package analyze

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/go-birds/photosift/internal/ingest"
	_ "modernc.org/sqlite"
)

func TestApplyLabelsDrivesUselessDetection(t *testing.T) {
	dir := t.TempDir()
	// A colourful, sharp image the heuristic would NOT call useless.
	p := writePNG(t, dir, "shelf.png", checkerboard(160, 120, 8, 0))

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

	// Without a label, heuristic should not flag this colourful image.
	sum, err := Run(db, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Counts[CatUseless] != 0 {
		t.Fatalf("expected no useless before labelling, got %d", sum.Counts[CatUseless])
	}

	// Apply a semantic label as the ML pass would.
	n, err := ApplyLabels(db, []Label{{Path: p, Label: "product in a store", Score: 0.92}})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 label applied, got %d", n)
	}

	sum, err = Run(db, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if sum.Counts[CatUseless] != 1 {
		t.Errorf("expected useless=1 after labelling, got %d", sum.Counts[CatUseless])
	}
}
