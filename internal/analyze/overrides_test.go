package analyze

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOverridesMissingFileIsOK(t *testing.T) {
	r, err := LoadOverrides("/no/such/file.json")
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected non-nil resolver for missing file")
	}
	if got := r.For("/anything"); got.BlurThreshold != nil {
		t.Errorf("expected empty override, got %+v", got)
	}
}

func TestLoadOverridesLongestPrefixWins(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "ov.json")
	body := `[
		{"prefix": "/a/b", "blur_threshold": 50},
		{"prefix": "/a/b/c", "blur_threshold": 30},
		{"prefix": "/a",   "blur_threshold": 80}
	]`
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := LoadOverrides(p)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		path string
		want float64
	}{
		{"/a/b/c/d.jpg", 30}, // deepest
		{"/a/b/x.jpg", 50},   // middle
		{"/a/x.jpg", 80},     // shallow
	}
	for _, c := range cases {
		got := r.For(c.path)
		if got.BlurThreshold == nil || *got.BlurThreshold != c.want {
			t.Errorf("For(%q) = %+v, want blur=%v", c.path, got, c.want)
		}
	}

	if got := r.For("/elsewhere/x.jpg"); got.BlurThreshold != nil {
		t.Errorf("expected empty override for unmatched path, got %+v", got)
	}
}
