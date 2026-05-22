// Package takeout reads Google Takeout export trees. Google Takeout writes a
// JSON "sidecar" next to each media file containing the original capture time,
// title, and (in some exports) a deep-link back to photos.google.com. The
// sidecar naming has changed across Takeout versions and is famously messy, so
// SidecarFor tries the known variants in order and returns the first that
// parses.
package takeout

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Meta is the subset of Takeout sidecar metadata we use.
type Meta struct {
	Title       string
	TakenAtUnix int64
	URL         string // photos.google.com deep-link, when present
}

type rawTime struct {
	Timestamp string `json:"timestamp"`
}

type rawMeta struct {
	Title          string  `json:"title"`
	PhotoTakenTime rawTime `json:"photoTakenTime"`
	CreationTime   rawTime `json:"creationTime"`
	URL            string  `json:"url"`
}

var dupSuffix = regexp.MustCompile(`\((\d+)\)$`)

// candidateSidecars returns plausible sidecar paths for a media file, most
// specific first. We cannot enumerate every Takeout quirk (long-name
// truncation in particular), but we cover the common cases:
//   - photo.jpg.json
//   - photo.jpg.supplemental-metadata.json   (newer exports)
//   - photo.json                              (extension dropped)
//   - duplicate markers: "photo(1).jpg" -> "photo.jpg(1).json"
func candidateSidecars(mediaPath string) []string {
	dir := filepath.Dir(mediaPath)
	base := filepath.Base(mediaPath)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	out := []string{
		mediaPath + ".json",
		mediaPath + ".supplemental-metadata.json",
		filepath.Join(dir, stem+".json"),
	}

	// Google moves the duplicate marker around: "IMG(1).jpg" pairs with
	// "IMG.jpg(1).json".
	if m := dupSuffix.FindStringSubmatch(stem); m != nil {
		realStem := dupSuffix.ReplaceAllString(stem, "")
		marker := "(" + m[1] + ")"
		out = append(out,
			filepath.Join(dir, realStem+ext+marker+".json"),
			filepath.Join(dir, realStem+ext+".supplemental-metadata"+marker+".json"),
		)
	}
	return out
}

// SidecarFor locates and parses the Takeout sidecar for a media file. The
// boolean is false when no sidecar is found, in which case callers should fall
// back to filesystem timestamps.
func SidecarFor(mediaPath string) (Meta, bool) {
	for _, c := range candidateSidecars(mediaPath) {
		b, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		var r rawMeta
		if err := json.Unmarshal(b, &r); err != nil {
			continue
		}
		m := Meta{Title: r.Title, URL: r.URL}
		ts := r.PhotoTakenTime.Timestamp
		if ts == "" {
			ts = r.CreationTime.Timestamp
		}
		if ts != "" {
			if n, err := strconv.ParseInt(ts, 10, 64); err == nil {
				m.TakenAtUnix = n
			}
		}
		return m, true
	}
	return Meta{}, false
}
