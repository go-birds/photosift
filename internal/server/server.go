// Package server exposes the analysis results over a small local HTTP API and
// serves the embedded review UI. It is intended to be bound to localhost and
// opened in a browser, giving photosift a desktop-app feel without a native
// toolchain.
package server

import (
	"bytes"
	"database/sql"
	"embed"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-birds/photosift/internal/analyze"
	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/image/draw"
)

//go:embed web
var webFS embed.FS

// thumbCacheSize bounds the in-memory thumbnail cache. 512 * ~6KB jpeg ≈ 3 MB
// resident — enough to keep an entire review session hot without blowing up
// on a 100k-photo library.
const thumbCacheSize = 512

// Server holds the dependencies for the review UI.
type Server struct {
	db        *sql.DB
	thumbs    *lru.Cache[int64, []byte] // bounded; persistent copy lives in DB
	thumbSide int

	// SSE subscribers fanning out summary updates whenever a decision
	// changes. Local app, single user — a tiny hub is plenty.
	subsMu sync.Mutex
	subs   map[chan struct{}]struct{}
}

// New builds a server backed by the given database handle.
func New(db *sql.DB) *Server {
	cache, _ := lru.New[int64, []byte](thumbCacheSize)
	return &Server{
		db:        db,
		thumbs:    cache,
		thumbSide: 320,
		subs:      map[chan struct{}]struct{}{},
	}
}

func (s *Server) subscribe() chan struct{} {
	ch := make(chan struct{}, 1)
	s.subsMu.Lock()
	s.subs[ch] = struct{}{}
	s.subsMu.Unlock()
	return ch
}

func (s *Server) unsubscribe(ch chan struct{}) {
	s.subsMu.Lock()
	delete(s.subs, ch)
	s.subsMu.Unlock()
}

// notify wakes every subscriber without blocking. A buffered channel of 1 means
// the wake-up is coalesced — multiple writes between flushes collapse to one
// pending notification.
func (s *Server) notify() {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Handler returns the HTTP handler for the whole app.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/groups", s.handleGroups)
	mux.HandleFunc("/api/thumb/", s.handleThumb)
	mux.HandleFunc("/api/image/", s.handleImage)
	mux.HandleFunc("/api/decision", s.handleDecision)
	mux.HandleFunc("/api/events", s.handleEvents)
	mux.HandleFunc("/api/export.csv", s.handleExportCSV)
	mux.HandleFunc("/api/export.json", s.handleExportJSON)
	return mux
}

type imageDTO struct {
	ID       int64   `json:"id"`
	Path     string  `json:"path"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
	BlurVar  float64 `json:"blur_var"`
	TakenAt  int64   `json:"taken_at"`
	URL      string  `json:"gphotos_url"`
	IsKeeper bool    `json:"is_keeper"`
	Reason   string  `json:"reason"`
	Score    float64 `json:"score"`
	Decision string  `json:"decision"`
}

type groupDTO struct {
	ClusterID int64      `json:"cluster_id"`
	Category  string     `json:"category"`
	Images    []imageDTO `json:"images"`
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) summary() (map[string]any, error) {
	rows, err := s.db.Query(`SELECT category, COUNT(*) FROM suggestions WHERE is_keeper=0 GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		counts[c] = n
	}

	var total, candidates, decided int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM images`).Scan(&total)
	_ = s.db.QueryRow(`SELECT COUNT(DISTINCT image_id) FROM suggestions WHERE is_keeper=0`).Scan(&candidates)
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&decided)
	return map[string]any{
		"total":      total,
		"candidates": candidates,
		"decided":    decided,
		"counts":     counts,
	}, nil
}

func (s *Server) handleSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := s.summary()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	writeJSON(w, sum)
}

// handleEvents is a Server-Sent Events stream. Whenever a decision changes,
// every connected client gets a fresh summary pushed — no polling, no
// per-click GETs. SSE was chosen over WebSocket because the traffic is
// strictly one-way and SSE works through proxies without negotiation.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable proxy buffering if any

	ch := s.subscribe()
	defer s.unsubscribe(ch)

	// Push the current summary immediately so the client doesn't need a
	// separate /api/summary fetch on connect.
	send := func() {
		sum, err := s.summary()
		if err != nil {
			return
		}
		data, _ := json.Marshal(sum)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(data)
		_, _ = w.Write([]byte("\n\n"))
		flusher.Flush()
	}
	send()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			send()
		}
	}
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	cat := r.URL.Query().Get("category")
	if cat == "" {
		http.Error(w, "category required", 400)
		return
	}

	rows, err := s.db.Query(`
		SELECT s.cluster_id, s.is_keeper, s.score, s.reason,
		       i.id, i.path, i.width, i.height, i.blur_var, i.taken_at_unix, i.gphotos_url,
		       COALESCE(d.action, '')
		FROM suggestions s
		JOIN images i ON i.id = s.image_id
		LEFT JOIN decisions d ON d.image_id = i.id
		WHERE s.category = ?
		ORDER BY s.cluster_id, s.is_keeper DESC, i.taken_at_unix`, cat)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer rows.Close()

	// Cluster 0 means "no cluster" (e.g. low quality); each such image is its
	// own single-item group. Initialised non-nil so an empty result encodes as
	// JSON [] instead of null.
	groups := []groupDTO{}
	byCluster := map[int64]int{} // cluster_id -> index into groups
	singleSeq := int64(-1)

	for rows.Next() {
		var clusterID int64
		var isKeeper int
		var img imageDTO
		if err := rows.Scan(&clusterID, &isKeeper, &img.Score, &img.Reason,
			&img.ID, &img.Path, &img.Width, &img.Height, &img.BlurVar,
			&img.TakenAt, &img.URL, &img.Decision); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		img.IsKeeper = isKeeper == 1

		key := clusterID
		if clusterID == 0 {
			key = singleSeq
			singleSeq--
		}
		idx, ok := byCluster[key]
		if !ok {
			groups = append(groups, groupDTO{ClusterID: clusterID, Category: cat})
			idx = len(groups) - 1
			byCluster[key] = idx
		}
		groups[idx].Images = append(groups[idx].Images, img)
	}
	writeJSON(w, groups)
}

func (s *Server) idFromPath(r *http.Request, prefix string) (int64, string, error) {
	idStr := r.URL.Path[len(prefix):]
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return 0, "", err
	}
	var path string
	err = s.db.QueryRow(`SELECT path FROM images WHERE id = ?`, id).Scan(&path)
	return id, path, err
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	_, path, err := s.idFromPath(r, "/api/image/")
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	http.ServeFile(w, r, path)
}

func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	id, path, err := s.idFromPath(r, "/api/thumb/")
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if cached, ok := s.thumbs.Get(id); ok {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(cached)
		return
	}

	// First try the thumbs table populated at scan time.
	var blob []byte
	if err := s.db.QueryRow(`SELECT jpeg FROM thumbs WHERE image_id = ?`, id).Scan(&blob); err == nil {
		s.thumbs.Add(id, blob)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(blob)
		return
	}

	// Fallback: generate on demand (databases scanned before thumbs existed,
	// or scan-time thumbnail generation failed). Persist for next time.
	buf, err := s.makeThumb(path)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	_, _ = s.db.Exec(`INSERT OR REPLACE INTO thumbs(image_id, jpeg) VALUES (?, ?)`, id, buf)
	s.thumbs.Add(id, buf)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(buf)
}

func (s *Server) makeThumb(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	side := s.thumbSide
	nw, nh := w, h
	if w >= h {
		if w > side {
			nw, nh = side, h*side/w
		}
	} else if h > side {
		nh, nw = side, w*side/h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)

	var out bytes.Buffer
	if err := jpeg.Encode(&out, dst, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", 405)
		return
	}
	var req struct {
		ImageID int64  `json:"image_id"`
		Action  string `json:"action"` // delete | keep | clear
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Action == "clear" {
		if _, err := s.db.Exec(`DELETE FROM decisions WHERE image_id = ?`, req.ImageID); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		s.notify()
		writeJSON(w, map[string]string{"status": "ok"})
		return
	}
	if req.Action != "delete" && req.Action != "keep" {
		http.Error(w, "action must be delete, keep or clear", 400)
		return
	}
	_, err := s.db.Exec(`INSERT INTO decisions(image_id, action, decided_at_unix)
		VALUES(?, ?, ?)
		ON CONFLICT(image_id) DO UPDATE SET action=excluded.action, decided_at_unix=excluded.decided_at_unix`,
		req.ImageID, req.Action, time.Now().Unix())
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	s.notify()
	writeJSON(w, map[string]string{"status": "ok"})
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	manifest, err := analyze.DeleteManifest(s.db)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=photosift-delete.json")
	writeJSON(w, manifest)
}

func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	manifest, err := analyze.DeleteManifest(s.db)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", "attachment; filename=photosift-delete.csv")
	cw := csv.NewWriter(w)
	cw.Write([]string{"id", "path", "dhash", "taken_at_unix", "gphotos_url"})
	for _, m := range manifest {
		cw.Write([]string{
			strconv.FormatInt(m.ID, 10), m.Path,
			strconv.FormatUint(m.Dhash, 10),
			strconv.FormatInt(m.TakenAt, 10), m.URL,
		})
	}
	cw.Flush()
}

// Serve starts the HTTP server on addr (blocking).
func Serve(db *sql.DB, addr string) error {
	srv := New(db)
	fmt.Printf("photosift review UI: http://%s\n", addr)
	return http.ListenAndServe(addr, srv.Handler())
}
