// index.go

package index

import "database/sql"

// Image is the analysis view of a stored image row.
type Image struct {
	ID           int64
	Path         string
	Sha256       string
	Dhash        uint64
	SizeBytes    int64
	Width        int
	Height       int
	BlurVar      float64
	Brightness   float64
	Colorfulness float64
	TakenAtUnix  int64
	GPhotosURL   string
	ContentLabel string
	ContentScore float64
}

// Pixels returns width*height, used to rank "best" image in a duplicate group.
func (i Image) Pixels() int64 { return int64(i.Width) * int64(i.Height) }

func LoadAllImages(db *sql.DB) ([]Image, error) {
	rows, err := db.Query(`SELECT id, path, sha256, dhash, size_bytes, width, height,
		blur_var, brightness, colorfulness, taken_at_unix, gphotos_url, content_label, content_score
		FROM images`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []Image
	for rows.Next() {
		var img Image
		var dh int64
		if err := rows.Scan(&img.ID, &img.Path, &img.Sha256, &dh, &img.SizeBytes,
			&img.Width, &img.Height, &img.BlurVar, &img.Brightness, &img.Colorfulness,
			&img.TakenAtUnix, &img.GPhotosURL, &img.ContentLabel, &img.ContentScore); err != nil {
			return nil, err
		}
		img.Dhash = uint64(dh)
		images = append(images, img)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return images, nil
}
