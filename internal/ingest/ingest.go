package ingest

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-birds/photosift/internal/takeout"
	_ "golang.org/x/image/webp"
)

// schemaSQL creates a fresh database. Existing databases are upgraded by
// ensureColumns so older scans keep working.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS images (
  id INTEGER PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,
  sha256 TEXT NOT NULL,
  dhash INTEGER NOT NULL,
  size_bytes INTEGER NOT NULL,
  mtime_unix INTEGER NOT NULL,
  scanned_at_unix INTEGER NOT NULL,
  width INTEGER NOT NULL DEFAULT 0,
  height INTEGER NOT NULL DEFAULT 0,
  blur_var REAL NOT NULL DEFAULT 0,
  brightness REAL NOT NULL DEFAULT 0,
  colorfulness REAL NOT NULL DEFAULT 0,
  taken_at_unix INTEGER NOT NULL DEFAULT 0,
  gphotos_url TEXT NOT NULL DEFAULT '',
  content_label TEXT NOT NULL DEFAULT '',
  content_score REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_images_sha256 ON images(sha256);
CREATE INDEX IF NOT EXISTS idx_images_dhash ON images(dhash);

CREATE TABLE IF NOT EXISTS suggestions (
  id INTEGER PRIMARY KEY,
  image_id INTEGER NOT NULL,
  category TEXT NOT NULL,
  score REAL NOT NULL,
  reason TEXT NOT NULL,
  cluster_id INTEGER NOT NULL DEFAULT 0,
  is_keeper INTEGER NOT NULL DEFAULT 0,
  UNIQUE(image_id, category)
);
CREATE INDEX IF NOT EXISTS idx_sugg_category ON suggestions(category);
CREATE INDEX IF NOT EXISTS idx_sugg_cluster ON suggestions(cluster_id);

CREATE TABLE IF NOT EXISTS decisions (
  image_id INTEGER PRIMARY KEY,
  action TEXT NOT NULL,
  decided_at_unix INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS thumbs (
  image_id INTEGER PRIMARY KEY,
  jpeg BLOB NOT NULL
);
`

// upgradeColumns are added to pre-existing image tables that predate the
// analysis feature.
var upgradeColumns = map[string]string{
	"width":         "INTEGER NOT NULL DEFAULT 0",
	"height":        "INTEGER NOT NULL DEFAULT 0",
	"blur_var":      "REAL NOT NULL DEFAULT 0",
	"brightness":    "REAL NOT NULL DEFAULT 0",
	"colorfulness":  "REAL NOT NULL DEFAULT 0",
	"taken_at_unix": "INTEGER NOT NULL DEFAULT 0",
	"gphotos_url":   "TEXT NOT NULL DEFAULT ''",
	"content_label": "TEXT NOT NULL DEFAULT ''",
	"content_score": "REAL NOT NULL DEFAULT 0",
}

// EnsureSchema creates tables, migrates old ones, and applies the pragmas that
// matter for bulk-write throughput.
func EnsureSchema(db *sql.DB) error {
	for _, p := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA busy_timeout=5000;",
	} {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	if _, err := db.Exec(schemaSQL); err != nil {
		return err
	}
	return ensureColumns(db)
}

func ensureColumns(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(images)`)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		have[name] = true
	}
	rows.Close()

	for col, def := range upgradeColumns {
		if !have[col] {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE images ADD COLUMN %s %s", col, def)); err != nil {
				return fmt.Errorf("add column %s: %w", col, err)
			}
		}
	}
	return nil
}

// exts is the set of file extensions we'll attempt to decode. HEIC/HEIF (the
// default iPhone format that often ends up in Takeout exports) is missing
// because Go has no pure-Go decoder for it — convert those to JPEG first.
var exts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".webp": true,
}

// record is a fully computed row handed from a worker to the DB writer.
type record struct {
	path                      string
	sha256                    string
	metrics                   Metrics
	sizeBytes, mtime, takenAt int64
	gphotosURL                string
	thumb                     []byte // JPEG bytes for the review UI
}

// thumbSize is the long-edge pixel size of the cached thumbnail. Matches
// internal/server's default so the UI gets the exact image it asks for.
const thumbSize = 320

// existing lets workers skip files whose size+mtime are unchanged since the
// last scan, avoiding the expensive decode entirely.
type existing struct {
	size  int64
	mtime int64
}

// ScanResult reports what a scan did.
type ScanResult struct {
	Scanned int // files processed (decoded + written)
	Skipped int // unchanged files skipped
	Failed  int // files we couldn't decode (e.g. HEIC, truncated jpegs)
	Total   int // rows in the table afterward
}

// ScanPaths walks roots, computes metrics for new/changed images in parallel,
// and writes them through a single batched writer goroutine. It returns once
// every file has been handled.
func ScanPaths(db *sql.DB, roots []string) (ScanResult, error) {
	prior, err := loadExisting(db)
	if err != nil {
		return ScanResult{}, fmt.Errorf("load existing: %w", err)
	}

	pathsCh := make(chan string, 1024)
	recCh := make(chan record, 1024)

	var scanned, skipped, failed int64
	workerCount := runtime.NumCPU()
	if workerCount < 1 {
		workerCount = 1
	}

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for i := 0; i < workerCount; i++ {
		go func() {
			defer wg.Done()
			for p := range pathsCh {
				rec, ok, err := processOne(p, prior)
				if err != nil {
					atomic.AddInt64(&failed, 1)
					continue
				}
				if !ok {
					atomic.AddInt64(&skipped, 1)
					continue
				}
				atomic.AddInt64(&scanned, 1)
				recCh <- rec
			}
		}()
	}

	// writer goroutine: single SQLite writer, batched into transactions.
	writerDone := make(chan error, 1)
	go func() { writerDone <- writeRecords(db, recCh) }()

	// walk producers
	go func() {
		defer close(pathsCh)
		for _, r := range roots {
			_ = filepath.WalkDir(r, func(path string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return nil
				}
				if exts[strings.ToLower(filepath.Ext(d.Name()))] {
					pathsCh <- path
				}
				return nil
			})
		}
	}()

	wg.Wait()
	close(recCh)
	if werr := <-writerDone; werr != nil {
		return ScanResult{}, werr
	}

	var total int
	_ = db.QueryRow(`SELECT COUNT(*) FROM images`).Scan(&total)
	return ScanResult{
		Scanned: int(atomic.LoadInt64(&scanned)),
		Skipped: int(atomic.LoadInt64(&skipped)),
		Failed:  int(atomic.LoadInt64(&failed)),
		Total:   total,
	}, nil
}

func loadExisting(db *sql.DB) (map[string]existing, error) {
	rows, err := db.Query(`SELECT path, size_bytes, mtime_unix FROM images`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]existing{}
	for rows.Next() {
		var p string
		var sz, mt int64
		if err := rows.Scan(&p, &sz, &mt); err != nil {
			return nil, err
		}
		out[p] = existing{size: sz, mtime: mt}
	}
	return out, rows.Err()
}

// processOne computes a record for a single image, or returns ok=false when the
// file is unchanged since the last scan and can be skipped.
func processOne(path string, prior map[string]existing) (record, bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return record{}, false, err
	}
	if e, ok := prior[path]; ok && e.size == fi.Size() && e.mtime == fi.ModTime().Unix() {
		return record{}, false, nil
	}

	f, err := os.Open(path)
	if err != nil {
		return record{}, false, err
	}
	defer f.Close()

	sha, err := sha256File(f)
	if err != nil {
		return record{}, false, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return record{}, false, err
	}

	img, _, err := image.Decode(f)
	if err != nil {
		return record{}, false, err
	}

	rec := record{
		path:      path,
		sha256:    sha,
		metrics:   computeMetrics(img),
		sizeBytes: fi.Size(),
		mtime:     fi.ModTime().Unix(),
		takenAt:   fi.ModTime().Unix(),
	}
	// Best-effort thumbnail; failure here is non-fatal (the server can
	// generate one on demand later).
	if thumb, err := thumbnailJPEG(img, thumbSize); err == nil {
		rec.thumb = thumb
	}
	if meta, ok := takeout.SidecarFor(path); ok {
		if meta.TakenAtUnix > 0 {
			rec.takenAt = meta.TakenAtUnix
		}
		rec.gphotosURL = meta.URL
	}
	return rec, true, nil
}

const batchSize = 500

func writeRecords(db *sql.DB, recCh <-chan record) error {
	const stmt = `INSERT INTO images
	  (path, sha256, dhash, size_bytes, mtime_unix, scanned_at_unix,
	   width, height, blur_var, brightness, colorfulness, taken_at_unix, gphotos_url)
	 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	 ON CONFLICT(path) DO UPDATE SET
	   sha256=excluded.sha256, dhash=excluded.dhash,
	   size_bytes=excluded.size_bytes, mtime_unix=excluded.mtime_unix,
	   scanned_at_unix=excluded.scanned_at_unix, width=excluded.width,
	   height=excluded.height, blur_var=excluded.blur_var,
	   brightness=excluded.brightness, colorfulness=excluded.colorfulness,
	   taken_at_unix=excluded.taken_at_unix, gphotos_url=excluded.gphotos_url`

	now := time.Now().Unix()
	batch := make([]record, 0, batchSize)

	// Resolves the image_id from the path the row was just upserted with.
	// Used to attach a thumbnail in the same transaction without needing
	// lastInsertId (which isn't reliable across ON CONFLICT DO UPDATE).
	const thumbStmt = `INSERT OR REPLACE INTO thumbs (image_id, jpeg)
	  VALUES ((SELECT id FROM images WHERE path = ?), ?)`

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		ps, err := tx.Prepare(stmt)
		if err != nil {
			tx.Rollback()
			return err
		}
		psThumb, err := tx.Prepare(thumbStmt)
		if err != nil {
			ps.Close()
			tx.Rollback()
			return err
		}
		for _, r := range batch {
			if _, err := ps.Exec(r.path, r.sha256, int64(r.metrics.Dhash),
				r.sizeBytes, r.mtime, now, r.metrics.Width, r.metrics.Height,
				r.metrics.BlurVar, r.metrics.Brightness, r.metrics.Colorfulness,
				r.takenAt, r.gphotosURL); err != nil {
				ps.Close()
				psThumb.Close()
				tx.Rollback()
				return err
			}
			if r.thumb != nil {
				if _, err := psThumb.Exec(r.path, r.thumb); err != nil {
					ps.Close()
					psThumb.Close()
					tx.Rollback()
					return err
				}
			}
		}
		ps.Close()
		psThumb.Close()
		if err := tx.Commit(); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}

	for r := range recCh {
		batch = append(batch, r)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	return flush()
}

func sha256File(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
