package ingest

import (
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"sync"
)

// HEIC/HEIF is the default capture format on iPhone. Go has no pure-Go
// decoder, so we shell out to whichever converter is installed:
//   - heif-convert (from libheif; brew/apt install libheif)
//   - sips (preinstalled on macOS)
// A scan without any converter will still complete; HEIC files just count as
// failed decodes and produce a one-shot hint pointing at the install steps.

type heicTool struct {
	bin  string
	args func(in, out string) []string
}

var (
	heicOnce       sync.Once
	heicConverter  *heicTool
	heicNoticeOnce sync.Once
)

func detectHEIC() {
	if p, err := exec.LookPath("heif-convert"); err == nil {
		heicConverter = &heicTool{
			bin:  p,
			args: func(in, out string) []string { return []string{in, out} },
		}
		return
	}
	if p, err := exec.LookPath("sips"); err == nil {
		heicConverter = &heicTool{
			bin: p,
			args: func(in, out string) []string {
				return []string{"-s", "format", "jpeg", in, "--out", out}
			},
		}
	}
}

// decodeHEIC converts a .heic/.heif file to a temp JPEG, decodes it, and
// removes the temp. The first failure from a missing converter prints a
// one-shot install hint.
func decodeHEIC(path string) (image.Image, error) {
	heicOnce.Do(detectHEIC)
	if heicConverter == nil {
		heicNoticeOnce.Do(func() {
			fmt.Fprintln(os.Stderr,
				"hint: HEIC files found but no converter installed. "+
					"Install libheif ('brew install libheif' or 'apt install libheif-examples') "+
					"or run on macOS to use the built-in 'sips'.")
		})
		return nil, fmt.Errorf("no HEIC converter")
	}
	tmp, err := os.CreateTemp("", "psift-heic-*.jpg")
	if err != nil {
		return nil, err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	cmd := exec.Command(heicConverter.bin, heicConverter.args(path, tmp.Name())...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("heic convert: %w (%s)", err, out)
	}

	f, err := os.Open(tmp.Name())
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return jpeg.Decode(f)
}
