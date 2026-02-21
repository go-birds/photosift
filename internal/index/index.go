// index.go

package index

import "database/sql"

type Image struct {
	Path   string
	Sha256 string
	Dhash  uint64
}

func LoadAllImages(db *sql.DB) ([]Image, error) {
	rows, err := db.Query("SELECT path, sha256, dhash FROM images")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var images []Image
	for rows.Next() {
		var img Image
		if err := rows.Scan(&img.Path, &img.Sha256, &img.Dhash); err != nil {
			return nil, err
		}
		images = append(images, img)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return images, nil
}
