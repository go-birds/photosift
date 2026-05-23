// Package analyze turns the indexed image metrics into deletion suggestions
// across five categories: exact duplicates, near duplicates, low quality,
// useless content, and over-shot subjects (too many photos of one thing).
package analyze

import (
	"database/sql"
	"fmt"
	"sort"

	"github.com/go-birds/photosift/internal/index"
)

// Suggestion categories.
const (
	CatExactDup   = "exact_duplicate"
	CatNearDup    = "near_duplicate"
	CatLowQuality = "low_quality"
	CatUseless    = "useless_content"
	CatOvershoot  = "overshoot"
)

// Options tunes the detectors. Zero values are replaced with defaults.
type Options struct {
	NearDist        int     // Hamming threshold for near-duplicates
	SubjectDist     int     // looser threshold grouping the same subject
	SubjectMin      int     // group size at which a subject is "over-shot"
	SubjectKeep     int     // how many to keep from an over-shot group
	BlurThreshold   float64 // blur variance below this is "blurry"
	DarkThreshold   float64 // mean brightness below this is "too dark"
	BrightThreshold float64 // mean brightness above this is "blown out"
	Classifier      Classifier
	NoLearn         bool // disable threshold adjustment from user decisions
}

func (o *Options) applyDefaults() {
	if o.NearDist == 0 {
		o.NearDist = 6
	}
	if o.SubjectDist == 0 {
		o.SubjectDist = 12
	}
	if o.SubjectMin == 0 {
		o.SubjectMin = 5
	}
	if o.SubjectKeep == 0 {
		o.SubjectKeep = 3
	}
	if o.BlurThreshold == 0 {
		o.BlurThreshold = 120
	}
	if o.DarkThreshold == 0 {
		o.DarkThreshold = 16
	}
	if o.BrightThreshold == 0 {
		o.BrightThreshold = 245
	}
	if o.Classifier == nil {
		o.Classifier = DefaultHeuristic()
	}
}

// Suggestion is one (image, category) finding.
type Suggestion struct {
	ImageID   int64
	Category  string
	Score     float64
	Reason    string
	ClusterID int64
	IsKeeper  bool
}

// Summary is a per-category count of deletion candidates (keepers excluded).
type Summary struct {
	Counts          map[string]int
	CandidateImages int      // distinct images suggested for deletion
	Total           int      // images analysed
	FeedbackLog     []string // adjustments learned from user decisions, if any
}

// Run analyses every indexed image and rewrites the suggestions table.
func Run(db *sql.DB, opts Options) (Summary, error) {
	opts.applyDefaults()

	var feedbackLog []string
	if !opts.NoLearn {
		log, err := applyFeedback(db, &opts)
		if err == nil {
			feedbackLog = log
		}
	}

	imgs, err := index.LoadAllImages(db)
	if err != nil {
		return Summary{}, fmt.Errorf("load images: %w", err)
	}

	var sugs []Suggestion
	var cluster int64

	hashIdx := newHashIndex(imgs)
	cluster = detectExactDup(imgs, &sugs, cluster)
	cluster = detectNearDup(imgs, hashIdx, opts, &sugs, cluster)
	cluster = detectOvershoot(imgs, hashIdx, opts, &sugs, cluster)
	detectLowQuality(imgs, opts, &sugs)
	detectUseless(imgs, opts, &sugs)

	if err := writeSuggestions(db, sugs); err != nil {
		return Summary{}, err
	}

	sum := Summary{Counts: map[string]int{}, Total: len(imgs)}
	candidates := map[int64]bool{}
	for _, s := range sugs {
		if s.IsKeeper {
			continue
		}
		sum.Counts[s.Category]++
		candidates[s.ImageID] = true
	}
	sum.CandidateImages = len(candidates)
	sum.FeedbackLog = feedbackLog
	return sum, nil
}

func detectExactDup(imgs []index.Image, out *[]Suggestion, cluster int64) int64 {
	bySha := map[string][]int{}
	for i, img := range imgs {
		bySha[img.Sha256] = append(bySha[img.Sha256], i)
	}
	// Stable iteration for deterministic cluster IDs.
	shas := make([]string, 0, len(bySha))
	for sha := range bySha {
		shas = append(shas, sha)
	}
	sort.Strings(shas)

	for _, sha := range shas {
		group := bySha[sha]
		if len(group) < 2 {
			continue
		}
		cluster++
		keeper := bestInGroup(imgs, group)
		for _, idx := range group {
			s := Suggestion{
				ImageID:   imgs[idx].ID,
				Category:  CatExactDup,
				Score:     1.0,
				ClusterID: cluster,
				IsKeeper:  idx == keeper,
			}
			if s.IsKeeper {
				s.Reason = fmt.Sprintf("best of %d byte-identical copies", len(group))
			} else {
				s.Reason = fmt.Sprintf("byte-identical to %s", short(imgs[keeper].Path))
			}
			*out = append(*out, s)
		}
	}
	return cluster
}

func detectNearDup(imgs []index.Image, idx *hashIndex, opts Options, out *[]Suggestion, cluster int64) int64 {
	for _, group := range idx.clusters(opts.NearDist) {
		cluster++
		keeper := bestInGroup(imgs, group)
		for _, idx := range group {
			s := Suggestion{
				ImageID:   imgs[idx].ID,
				Category:  CatNearDup,
				Score:     0.9,
				ClusterID: cluster,
				IsKeeper:  idx == keeper,
			}
			if s.IsKeeper {
				s.Reason = fmt.Sprintf("sharpest of %d near-identical frames", len(group))
			} else {
				s.Reason = fmt.Sprintf("near-identical to %s", short(imgs[keeper].Path))
			}
			*out = append(*out, s)
		}
	}
	return cluster
}

// detectOvershoot flags the surplus when many photos share a subject. It keeps
// the best SubjectKeep and suggests deleting the rest.
func detectOvershoot(imgs []index.Image, idx *hashIndex, opts Options, out *[]Suggestion, cluster int64) int64 {
	for _, group := range idx.clusters(opts.SubjectDist) {
		if len(group) < opts.SubjectMin {
			continue
		}
		cluster++
		// Rank the group best-first and keep the top SubjectKeep.
		ranked := append([]int(nil), group...)
		sort.Slice(ranked, func(a, b int) bool {
			return better(imgs[ranked[a]], imgs[ranked[b]])
		})
		keep := map[int]bool{}
		for i := 0; i < opts.SubjectKeep && i < len(ranked); i++ {
			keep[ranked[i]] = true
		}
		for _, idx := range ranked {
			isKeeper := keep[idx]
			s := Suggestion{
				ImageID:   imgs[idx].ID,
				Category:  CatOvershoot,
				Score:     0.6,
				ClusterID: cluster,
				IsKeeper:  isKeeper,
			}
			if isKeeper {
				s.Reason = fmt.Sprintf("keeper from a set of %d similar photos", len(group))
			} else {
				s.Reason = fmt.Sprintf("1 of %d photos of the same subject", len(group))
			}
			*out = append(*out, s)
		}
	}
	return cluster
}

func detectLowQuality(imgs []index.Image, opts Options, out *[]Suggestion) {
	for _, img := range imgs {
		switch {
		case img.BlurVar > 0 && img.BlurVar < opts.BlurThreshold:
			score := clamp01((opts.BlurThreshold - img.BlurVar) / opts.BlurThreshold)
			*out = append(*out, Suggestion{
				ImageID: img.ID, Category: CatLowQuality, Score: score,
				Reason: fmt.Sprintf("blurry / soft focus (sharpness %.0f)", img.BlurVar),
			})
		case img.Brightness > 0 && img.Brightness < opts.DarkThreshold:
			*out = append(*out, Suggestion{
				ImageID: img.ID, Category: CatLowQuality, Score: 0.7,
				Reason: fmt.Sprintf("very dark (brightness %.0f)", img.Brightness),
			})
		case img.Brightness > opts.BrightThreshold:
			*out = append(*out, Suggestion{
				ImageID: img.ID, Category: CatLowQuality, Score: 0.7,
				Reason: fmt.Sprintf("over-exposed (brightness %.0f)", img.Brightness),
			})
		}
	}
}

func detectUseless(imgs []index.Image, opts Options, out *[]Suggestion) {
	for _, img := range imgs {
		label, score, useless := opts.Classifier.Classify(img)
		if useless {
			*out = append(*out, Suggestion{
				ImageID: img.ID, Category: CatUseless, Score: score,
				Reason: label,
			})
		}
	}
}

func writeSuggestions(db *sql.DB, sugs []Suggestion) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM suggestions`); err != nil {
		tx.Rollback()
		return err
	}
	ps, err := tx.Prepare(`INSERT OR REPLACE INTO suggestions
		(image_id, category, score, reason, cluster_id, is_keeper)
		VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		tx.Rollback()
		return err
	}
	for _, s := range sugs {
		keeper := 0
		if s.IsKeeper {
			keeper = 1
		}
		if _, err := ps.Exec(s.ImageID, s.Category, s.Score, s.Reason, s.ClusterID, keeper); err != nil {
			ps.Close()
			tx.Rollback()
			return err
		}
	}
	ps.Close()
	return tx.Commit()
}

func short(path string) string {
	const max = 48
	if len(path) <= max {
		return path
	}
	return "..." + path[len(path)-max:]
}
