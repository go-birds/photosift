package analyze

import (
	"database/sql"
	"fmt"
)

// minFeedbackSample is the smallest number of user decisions in a category
// before learned adjustments kick in. Below this the rate is too noisy to
// trust.
const minFeedbackSample = 20

// keepRateTrigger is the fraction of "keep" decisions among a category's
// flagged photos at which we conclude the threshold is too aggressive. Half is
// the natural cutoff: the user is overriding us more often than not.
const keepRateTrigger = 0.5

// applyFeedback tightens thresholds for categories the user has been
// over-riding, based on decisions logged from previous review sessions. It
// mutates opts and returns a human-readable log; an empty log means there
// wasn't enough signal to act on.
//
// Only the two categories with explicit numeric thresholds learn: low_quality
// (blur threshold) and useless_content (classifier threshold). The clustering
// categories (exact/near dup, overshoot) are structural — keeping a flagged
// duplicate doesn't mean the cluster was wrong, just that the user wanted both.
func applyFeedback(db *sql.DB, opts *Options) ([]string, error) {
	counts, err := countDecisionsByCategory(db)
	if err != nil {
		return nil, err
	}

	var log []string

	if c := counts[CatLowQuality]; c.Total >= minFeedbackSample {
		rate := float64(c.Kept) / float64(c.Total)
		if rate > keepRateTrigger {
			old := opts.BlurThreshold
			// Multiplicatively shrink toward "only the really blurry stuff".
			opts.BlurThreshold *= 0.75
			log = append(log, fmt.Sprintf(
				"blur threshold tightened %.0f -> %.0f (%d of %d low_quality flags kept)",
				old, opts.BlurThreshold, c.Kept, c.Total))
		}
	}

	if c := counts[CatUseless]; c.Total >= minFeedbackSample {
		rate := float64(c.Kept) / float64(c.Total)
		if rate > keepRateTrigger {
			if hc, ok := opts.Classifier.(HeuristicClassifier); ok {
				old := hc.Threshold
				// Raising the score threshold means only higher-confidence
				// outputs are flagged.
				hc.Threshold = clamp01(hc.Threshold * 1.15)
				opts.Classifier = hc
				log = append(log, fmt.Sprintf(
					"useless threshold raised %.2f -> %.2f (%d of %d useless_content flags kept)",
					old, hc.Threshold, c.Kept, c.Total))
			}
		}
	}

	return log, nil
}

type catCounts struct{ Total, Kept int }

// countDecisionsByCategory returns, per category, how many of its non-keeper
// suggestions the user has decided on and how many of those they marked as
// "keep" (i.e. overrides of our recommendation).
func countDecisionsByCategory(db *sql.DB) (map[string]catCounts, error) {
	rows, err := db.Query(`
		SELECT s.category, d.action, COUNT(*)
		FROM suggestions s
		JOIN decisions d ON d.image_id = s.image_id
		WHERE s.is_keeper = 0
		GROUP BY s.category, d.action`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]catCounts{}
	for rows.Next() {
		var cat, action string
		var n int
		if err := rows.Scan(&cat, &action, &n); err != nil {
			return nil, err
		}
		c := out[cat]
		c.Total += n
		if action == "keep" {
			c.Kept += n
		}
		out[cat] = c
	}
	return out, rows.Err()
}
