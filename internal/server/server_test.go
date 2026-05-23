package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-birds/photosift/internal/analyze"
	"github.com/go-birds/photosift/internal/ingest"
	_ "modernc.org/sqlite"
)

// seedDB scans a tiny synthetic library and runs analyze so the handlers have
// real rows to read back. Keeping the seed minimal (3 files, one obvious
// duplicate) makes the assertions stable.
func seedDB(t *testing.T) (*sql.DB, int64) {
	t.Helper()
	dir := t.TempDir()
	for i, name := range []string{"a.png", "a_copy.png", "b.png"} {
		img := image.NewRGBA(image.Rect(0, 0, 64, 64))
		fill := color.RGBA{200, 30, 30, 255}
		if i == 2 {
			fill = color.RGBA{30, 200, 30, 255}
		}
		for y := 0; y < 64; y++ {
			for x := 0; x < 64; x++ {
				img.Set(x, y, fill)
			}
		}
		// First two share the same bytes -> exact duplicates.
		var p string
		if i == 0 {
			p = filepath.Join(dir, name)
			f, _ := os.Create(p)
			png.Encode(f, img)
			f.Close()
		} else if i == 1 {
			b, _ := os.ReadFile(filepath.Join(dir, "a.png"))
			os.WriteFile(filepath.Join(dir, name), b, 0o644)
		} else {
			f, _ := os.Create(filepath.Join(dir, name))
			png.Encode(f, img)
			f.Close()
		}
	}

	db, err := sql.Open("sqlite", filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := ingest.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.ScanPaths(db, []string{dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := analyze.Run(db, analyze.Options{NoLearn: true}); err != nil {
		t.Fatal(err)
	}

	// Pluck one non-keeper image id for decision tests.
	var id int64
	if err := db.QueryRow(`SELECT image_id FROM suggestions WHERE is_keeper = 0 LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return db, id
}

func TestSummaryShape(t *testing.T) {
	db, _ := seedDB(t)
	defer db.Close()
	srv := New(db)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/summary", nil))

	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Total      int            `json:"total"`
		Candidates int            `json:"candidates"`
		Decided    int            `json:"decided"`
		Counts     map[string]int `json:"counts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Total != 3 {
		t.Errorf("total = %d, want 3", got.Total)
	}
	if got.Counts[analyze.CatExactDup] < 1 {
		t.Errorf("expected exact_duplicate count, got %d", got.Counts[analyze.CatExactDup])
	}
}

func TestGroupsReturnsClusteredImages(t *testing.T) {
	db, _ := seedDB(t)
	defer db.Close()
	srv := New(db)

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET",
		"/api/groups?category=exact_duplicate", nil))

	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var groups []struct {
		ClusterID int64 `json:"cluster_id"`
		Images    []struct {
			ID       int64  `json:"id"`
			IsKeeper bool   `json:"is_keeper"`
			Path     string `json:"path"`
		} `json:"images"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&groups); err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || len(groups[0].Images) != 2 {
		t.Fatalf("expected 1 group of 2, got %+v", groups)
	}
	keepers := 0
	for _, im := range groups[0].Images {
		if im.IsKeeper {
			keepers++
		}
	}
	if keepers != 1 {
		t.Errorf("expected exactly 1 keeper, got %d", keepers)
	}
}

func TestDecisionRoundTripAndExport(t *testing.T) {
	db, id := seedDB(t)
	defer db.Close()
	srv := New(db)
	mux := srv.Handler()

	// Mark a delete decision via the API.
	post := httptest.NewRequest("POST", "/api/decision",
		strings.NewReader(`{"image_id":`+itoa(id)+`,"action":"delete"}`))
	post.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, post)
	if rec.Code != 200 {
		t.Fatalf("decision status %d body=%s", rec.Code, rec.Body.String())
	}

	// Export should now show only that image.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/export.json", nil))
	if rec.Code != 200 {
		t.Fatalf("export status %d", rec.Code)
	}
	var manifest []struct {
		ID int64 `json:"id"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest) != 1 || manifest[0].ID != id {
		t.Fatalf("expected manifest with only id=%d, got %+v", id, manifest)
	}
}

func TestEventsStreamsOnDecision(t *testing.T) {
	db, id := seedDB(t)
	defer db.Close()
	srv := New(db)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Trigger a decision once the stream is open so a push fires.
	go func() {
		time.Sleep(150 * time.Millisecond)
		http.Post(ts.URL+"/api/decision", "application/json",
			strings.NewReader(`{"image_id":`+itoa(id)+`,"action":"delete"}`))
	}()

	buf := make([]byte, 4096)
	frames := 0
	deadline := time.After(2 * time.Second)
loop:
	for {
		select {
		case <-deadline:
			break loop
		default:
			n, err := resp.Body.Read(buf)
			if n > 0 {
				frames += strings.Count(string(buf[:n]), "data: ")
				if frames >= 2 {
					break loop
				}
			}
			if err != nil {
				break loop
			}
		}
	}
	if frames < 2 {
		t.Errorf("expected at least 2 SSE frames (connect + decision), got %d", frames)
	}
}

func itoa(n int64) string {
	var s [20]byte
	i := len(s)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		i--
		s[i] = byte('0' + n%10)
		n /= 10
	}
	return string(s[i:])
}
