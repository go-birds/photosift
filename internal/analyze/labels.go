package analyze

import "database/sql"

// Label is a semantic classification for one image, produced by an external ML
// model (see ml/classify.py). photosift keeps the hot scan/analyze path
// pure-Go and fast; semantic content labelling runs as a separate, optional
// pass whose output is imported here and consumed by the "useless content"
// detector.
type Label struct {
	Path  string  `json:"path"`
	Label string  `json:"label"`
	Score float64 `json:"score"`
}

// ApplyLabels writes model labels onto the matching image rows. It matches by
// absolute path and returns the number of rows updated.
func ApplyLabels(db *sql.DB, labels []Label) (int, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	ps, err := tx.Prepare(`UPDATE images SET content_label=?, content_score=? WHERE path=?`)
	if err != nil {
		tx.Rollback()
		return 0, err
	}
	updated := 0
	for _, l := range labels {
		res, err := ps.Exec(l.Label, l.Score, l.Path)
		if err != nil {
			ps.Close()
			tx.Rollback()
			return 0, err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			updated += int(n)
		}
	}
	ps.Close()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return updated, nil
}
