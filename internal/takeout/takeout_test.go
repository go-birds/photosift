package takeout

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSidecarVariants(t *testing.T) {
	dir := t.TempDir()
	json := `{"title":"IMG_1.jpg","photoTakenTime":{"timestamp":"1600000000"},"url":"https://photos.google.com/x"}`

	cases := []struct {
		media   string
		sidecar string
	}{
		{"IMG_1.jpg", "IMG_1.jpg.json"},
		{"IMG_2.jpg", "IMG_2.jpg.supplemental-metadata.json"},
		{"IMG_3.jpg", "IMG_3.json"},
		{"IMG_4(1).jpg", "IMG_4.jpg(1).json"},
	}
	for _, c := range cases {
		media := filepath.Join(dir, c.media)
		if err := os.WriteFile(media, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, c.sidecar), []byte(json), 0o644); err != nil {
			t.Fatal(err)
		}
		meta, ok := SidecarFor(media)
		if !ok {
			t.Errorf("%s: no sidecar found (expected %s)", c.media, c.sidecar)
			continue
		}
		if meta.TakenAtUnix != 1600000000 {
			t.Errorf("%s: takenAt = %d", c.media, meta.TakenAtUnix)
		}
		if meta.URL == "" {
			t.Errorf("%s: missing url", c.media)
		}
	}

	if _, ok := SidecarFor(filepath.Join(dir, "no_sidecar.jpg")); ok {
		t.Error("expected no sidecar for unmatched file")
	}
}
