package analyze

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// Override sets per-album threshold tweaks. Each entry matches images by path
// prefix; the longest matching prefix wins so you can nest narrow rules inside
// broad ones. Any field left null inherits from the global Options.
//
// Example overrides.json:
//
//	[
//	  {"prefix": "/photos/Italy 2024",     "blur_threshold": 80},
//	  {"prefix": "/photos/screenshots",    "blur_threshold": 30},
//	  {"prefix": "/photos/family/quick",   "useless_threshold": 0.7}
//	]
type Override struct {
	Prefix           string   `json:"prefix"`
	BlurThreshold    *float64 `json:"blur_threshold,omitempty"`
	DarkThreshold    *float64 `json:"dark_threshold,omitempty"`
	BrightThreshold  *float64 `json:"bright_threshold,omitempty"`
	UselessThreshold *float64 `json:"useless_threshold,omitempty"`
}

// Resolver picks the most specific Override for a given image path.
type Resolver struct{ rules []Override }

// LoadOverrides reads an overrides JSON file. A missing file is not an error —
// a Resolver with no rules is returned and every lookup falls through to
// defaults.
func LoadOverrides(path string) (*Resolver, error) {
	if path == "" {
		return &Resolver{}, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Resolver{}, nil
		}
		return nil, err
	}
	var rules []Override
	if err := json.Unmarshal(b, &rules); err != nil {
		return nil, err
	}
	// Longest prefix first so For() can return on the first match.
	sort.Slice(rules, func(i, j int) bool {
		return len(rules[i].Prefix) > len(rules[j].Prefix)
	})
	return &Resolver{rules: rules}, nil
}

// For returns the longest-matching override for an image path, or an empty
// Override (all nil pointers) when no rule matches.
func (r *Resolver) For(imgPath string) Override {
	if r == nil {
		return Override{}
	}
	for _, rule := range r.rules {
		if strings.HasPrefix(imgPath, rule.Prefix) {
			return rule
		}
	}
	return Override{}
}

// effectiveBlur returns the blur threshold for an image: per-album override if
// present, else the global default.
func effectiveBlur(ov Override, def float64) float64 {
	if ov.BlurThreshold != nil {
		return *ov.BlurThreshold
	}
	return def
}

func effectiveDark(ov Override, def float64) float64 {
	if ov.DarkThreshold != nil {
		return *ov.DarkThreshold
	}
	return def
}

func effectiveBright(ov Override, def float64) float64 {
	if ov.BrightThreshold != nil {
		return *ov.BrightThreshold
	}
	return def
}

func effectiveUseless(ov Override, def float64) float64 {
	if ov.UselessThreshold != nil {
		return *ov.UselessThreshold
	}
	return def
}
