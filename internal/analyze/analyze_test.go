package analyze

import (
	"database/sql"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-birds/photosift/internal/ingest"
	_ "modernc.org/sqlite"
)

// writePNG renders img to a file under dir.
func writePNG(t *testing.T, dir, name string, img image.Image) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return p
}

// checkerboard is sharp and colourful (a "good" photo).
func checkerboard(w, h, cell int, shift int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if ((x+shift)/cell+y/cell)%2 == 0 {
				img.Set(x, y, color.RGBA{220, 40, 30, 255})
			} else {
				img.Set(x, y, color.RGBA{20, 60, 210, 255})
			}
		}
	}
	return img
}

// flat is a near-uniform grey image: low edge energy => blurry.
func flat(w, h int, lum uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// tiny ramp to avoid a perfectly constant image
			v := lum + uint8((x+y)%2)
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	return img
}

// brightDoc is a near-white, low-colour image with thin dark lines: a document.
func brightDoc(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{248, 248, 246, 255}
			if y%12 == 0 && x%3 == 0 {
				c = color.RGBA{30, 30, 30, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunDetectsCategories(t *testing.T) {
	dir := t.TempDir()

	// Exact duplicate: identical bytes.
	a := writePNG(t, dir, "a.png", checkerboard(128, 128, 8, 0))
	copyFile(t, a, filepath.Join(dir, "a_copy.png"))

	// A cluster of many near-identical frames (over-shot subject + near dup).
	for i := 0; i < 6; i++ {
		writePNG(t, dir, "burst_"+string(rune('a'+i))+".png", checkerboard(160, 120, 10, i))
	}

	// Blurry image.
	writePNG(t, dir, "blurry.png", flat(128, 128, 128))

	// Bright document.
	writePNG(t, dir, "doc.png", brightDoc(120, 160))

	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := ingest.EnsureSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := ingest.ScanPaths(db, []string{dir}); err != nil {
		t.Fatal(err)
	}

	sum, err := Run(db, Options{})
	if err != nil {
		t.Fatal(err)
	}

	if sum.Counts[CatExactDup] < 1 {
		t.Errorf("expected an exact duplicate, got %d", sum.Counts[CatExactDup])
	}
	if sum.Counts[CatNearDup] < 1 {
		t.Errorf("expected near duplicates, got %d", sum.Counts[CatNearDup])
	}
	if sum.Counts[CatLowQuality] < 1 {
		t.Errorf("expected a low-quality image, got %d", sum.Counts[CatLowQuality])
	}
	if sum.Counts[CatUseless] < 1 {
		t.Errorf("expected a useless/document image, got %d", sum.Counts[CatUseless])
	}
	if sum.CandidateImages == 0 {
		t.Error("expected at least one deletion candidate")
	}
	t.Logf("summary: %+v", sum.Counts)
}
