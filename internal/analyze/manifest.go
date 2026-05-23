package analyze

import "database/sql"

// ManifestEntry describes one photo slated for deletion. dhash and taken-at are
// included so the browser automation can locate the photo in the Google Photos
// timeline by date + perceptual match.
type ManifestEntry struct {
	ID      int64  `json:"id"`
	Path    string `json:"path"`
	Dhash   uint64 `json:"dhash"`
	TakenAt int64  `json:"taken_at"`
	URL     string `json:"gphotos_url"`
}

// DeleteManifest returns the photos confirmed for deletion. When the user has
// made explicit decisions it returns only those marked "delete"; otherwise it
// falls back to every non-keeper candidate so callers always get the full set.
func DeleteManifest(db *sql.DB) ([]ManifestEntry, error) {
	var n int
	_ = db.QueryRow(`SELECT COUNT(*) FROM decisions WHERE action='delete'`).Scan(&n)

	var rows *sql.Rows
	var err error
	if n > 0 {
		rows, err = db.Query(`
			SELECT i.id, i.path, i.dhash, i.taken_at_unix, i.gphotos_url
			FROM decisions d JOIN images i ON i.id = d.image_id
			WHERE d.action='delete' ORDER BY i.taken_at_unix`)
	} else {
		rows, err = db.Query(`
			SELECT DISTINCT i.id, i.path, i.dhash, i.taken_at_unix, i.gphotos_url
			FROM suggestions s JOIN images i ON i.id = s.image_id
			WHERE s.is_keeper=0 ORDER BY i.taken_at_unix`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ManifestEntry
	for rows.Next() {
		var m ManifestEntry
		var dh int64
		if err := rows.Scan(&m.ID, &m.Path, &dh, &m.TakenAt, &m.URL); err != nil {
			return nil, err
		}
		m.Dhash = uint64(dh)
		out = append(out, m)
	}
	return out, rows.Err()
}
