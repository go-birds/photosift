package analyze

import (
	"strings"

	"github.com/go-birds/photosift/internal/index"
)

// Classifier decides whether an image is "useless content" (category 4:
// documents, screenshots, photos of products in a store, a broken part you
// photographed to match at the hardware store, etc.).
//
// Two implementations exist:
//   - HeuristicClassifier: pure-Go, always available, works from the cheap
//     pixel statistics computed at scan time. It reliably catches documents,
//     receipts and screenshots (low colour, bright, text-edge heavy) but cannot
//     reason about object semantics.
//   - the optional ML pass (see internal/analyze/ml.go, build tag "onnx")
//     writes a semantic label into images.content_label; when present that
//     label is trusted over the heuristic.
type Classifier interface {
	Classify(img index.Image) (label string, score float64, useless bool)
}

// uselessLabels maps semantic labels (produced by the ML pass) to the "useless
// content" bucket. Keys are matched as case-insensitive substrings so a single
// entry covers related ImageNet/CLIP label spellings.
var uselessLabels = []string{
	"document", "menu", "receipt", "web site", "website", "screenshot",
	"envelope", "book jacket", "comic book", "packet", "carton", "binder",
	"price tag", "barcode", "label", "shopping", "packaging", "product",
	"monitor", "screen", "spreadsheet", "text",
}

// HeuristicClassifier classifies from scan-time pixel statistics only.
type HeuristicClassifier struct {
	// MinColor and MinBright define the document/screenshot region: low colour
	// with a bright background. Tunable for experimentation.
	MaxColorfulness float64
	MinBrightness   float64
	Threshold       float64 // useless when score exceeds this
}

// DefaultHeuristic returns a classifier with sensible starting thresholds.
func DefaultHeuristic() HeuristicClassifier {
	return HeuristicClassifier{MaxColorfulness: 20, MinBrightness: 165, Threshold: 0.45}
}

func (h HeuristicClassifier) Classify(img index.Image) (string, float64, bool) {
	// Trust a semantic label from the ML pass when one exists.
	if img.ContentLabel != "" {
		low := strings.ToLower(img.ContentLabel)
		for _, k := range uselessLabels {
			if strings.Contains(low, k) {
				return img.ContentLabel, img.ContentScore, img.ContentScore >= h.Threshold
			}
		}
		return img.ContentLabel, img.ContentScore, false
	}

	// Heuristic: documents/receipts/screenshots are low-colour with a bright
	// background. Combine the two signals into a 0..1 score.
	colorScore := clamp01((h.MaxColorfulness - img.Colorfulness) / h.MaxColorfulness)
	brightScore := clamp01((img.Brightness - h.MinBrightness) / (255 - h.MinBrightness))
	score := 0.5*colorScore + 0.5*brightScore
	if colorScore == 0 || brightScore == 0 {
		// Both conditions must hold to call something a document.
		score = 0
	}
	return "document/screenshot (heuristic)", score, score >= h.Threshold
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
