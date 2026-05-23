package analyze

import "github.com/go-birds/photosift/internal/index"

// Classifier decides whether an image is "useless content" (documents,
// receipts, screenshots, photos of products in a store, broken parts you
// photographed for the hardware store...).
//
// Two stages cooperate:
//   - When an image has been labelled by the optional ML pass (ml/classify.py,
//     imported via `photosift tag`), we trust that label. ML can make semantic
//     judgements pixel statistics cannot.
//   - Otherwise we fall back to a cheap heuristic over the pixel statistics
//     captured at scan time: documents and receipts are bright and
//     near-grayscale.
type Classifier interface {
	Classify(img index.Image) (label string, score float64, useless bool)
}

// uselessBuckets are the labels both pipelines agree mean "deletion candidate".
// To add a new useless category, add it here AND to USELESS_PROMPTS in
// ml/classify.py (the Go side does exact-match lookup, not substring).
var uselessBuckets = map[string]bool{
	"document":               true,
	"receipt":                true,
	"screenshot":             true,
	"product in a store":     true,
	"broken item to replace": true,
	"whiteboard or notes":    true,
}

// HeuristicClassifier scores documents/receipts/screenshots from pixel stats.
type HeuristicClassifier struct {
	MaxColorfulness float64 // colourfulness below this counts as near-grayscale
	MinBrightness   float64 // mean luma above this counts as a bright background
	Threshold       float64 // combined score above this is "useless"
}

func DefaultHeuristic() HeuristicClassifier {
	return HeuristicClassifier{MaxColorfulness: 20, MinBrightness: 165, Threshold: 0.45}
}

func (h HeuristicClassifier) Classify(img index.Image) (string, float64, bool) {
	if img.ContentLabel != "" {
		return img.ContentLabel, img.ContentScore, uselessBuckets[img.ContentLabel]
	}

	colorScore := clamp01((h.MaxColorfulness - img.Colorfulness) / h.MaxColorfulness)
	brightScore := clamp01((img.Brightness - h.MinBrightness) / (255 - h.MinBrightness))
	// Require BOTH signals — a low-colour-only or bright-only image is not
	// necessarily a document.
	if colorScore == 0 || brightScore == 0 {
		return "", 0, false
	}
	score := 0.5*colorScore + 0.5*brightScore
	return "document", score, score >= h.Threshold
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
