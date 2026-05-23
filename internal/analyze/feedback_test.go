package analyze

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func setupSchema(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{
		`CREATE TABLE images (id INTEGER PRIMARY KEY, path TEXT, content_label TEXT DEFAULT '')`,
		`CREATE TABLE suggestions (id INTEGER PRIMARY KEY, image_id INTEGER, category TEXT, is_keeper INTEGER DEFAULT 0)`,
		`CREATE TABLE decisions (image_id INTEGER PRIMARY KEY, action TEXT, decided_at_unix INTEGER)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestApplyFeedbackTightensBlurThreshold(t *testing.T) {
	db := setupSchema(t)
	defer db.Close()

	// Seed: 25 low_quality suggestions, user kept 18 of them (> 50% override).
	for i := 0; i < 25; i++ {
		db.Exec(`INSERT INTO images(id, path) VALUES (?, ?)`, i, "p")
		db.Exec(`INSERT INTO suggestions(image_id, category, is_keeper) VALUES (?, 'low_quality', 0)`, i)
		action := "delete"
		if i < 18 {
			action = "keep"
		}
		db.Exec(`INSERT INTO decisions(image_id, action, decided_at_unix) VALUES (?, ?, 0)`, i, action)
	}

	opts := Options{}
	opts.applyDefaults()
	original := opts.BlurThreshold

	log, err := applyFeedback(db, &opts)
	if err != nil {
		t.Fatal(err)
	}
	if opts.BlurThreshold >= original {
		t.Errorf("expected blur threshold to drop, stayed at %v", opts.BlurThreshold)
	}
	if len(log) == 0 {
		t.Error("expected feedback log entry, got none")
	}
}

func TestApplyFeedbackBelowSampleSizeDoesNothing(t *testing.T) {
	db := setupSchema(t)
	defer db.Close()

	// Only 5 decisions — below the threshold, so no change.
	for i := 0; i < 5; i++ {
		db.Exec(`INSERT INTO images(id, path) VALUES (?, ?)`, i, "p")
		db.Exec(`INSERT INTO suggestions(image_id, category, is_keeper) VALUES (?, 'low_quality', 0)`, i)
		db.Exec(`INSERT INTO decisions(image_id, action, decided_at_unix) VALUES (?, 'keep', 0)`, i)
	}

	opts := Options{}
	opts.applyDefaults()
	original := opts.BlurThreshold

	log, err := applyFeedback(db, &opts)
	if err != nil {
		t.Fatal(err)
	}
	if opts.BlurThreshold != original {
		t.Errorf("expected no adjustment with small sample, got %v -> %v", original, opts.BlurThreshold)
	}
	if len(log) != 0 {
		t.Errorf("expected empty log, got %v", log)
	}
}
