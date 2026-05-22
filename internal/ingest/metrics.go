package ingest

import (
	"image"
	"math"

	"golang.org/x/image/draw"
)

// Metrics are the per-image numbers we derive from a single decode pass.
type Metrics struct {
	Dhash        uint64
	Width        int
	Height       int
	BlurVar      float64 // variance of the Laplacian; lower = blurrier
	Brightness   float64 // mean luma, 0-255
	Colorfulness float64 // Hasler-Susstrunk colorfulness; low => grayscale/document
}

// the side length we downscale to before computing pixel statistics. Small
// enough to be cheap, large enough to preserve edge energy.
const workSize = 256

// computeMetrics derives every cheap signal we need from one decoded image. We
// downscale to RGBA exactly once and compute every statistic from that buffer
// so each file is decoded and resized a single time.
func computeMetrics(img image.Image) Metrics {
	b := img.Bounds()
	rgba := scaledRGBA(img, workSize)
	w, h := rgba.Rect.Dx(), rgba.Rect.Dy()

	gray := make([]float64, w*h)
	var sumLuma, sumRG, sumYB, sumRG2, sumYB2 float64
	n := float64(w * h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := rgba.PixOffset(x, y)
			r := float64(rgba.Pix[i])
			g := float64(rgba.Pix[i+1])
			bl := float64(rgba.Pix[i+2])
			luma := 0.299*r + 0.587*g + 0.114*bl
			gray[y*w+x] = luma
			sumLuma += luma
			rg := r - g
			yb := 0.5*(r+g) - bl
			sumRG += rg
			sumYB += yb
			sumRG2 += rg * rg
			sumYB2 += yb * yb
		}
	}

	stdRG := math.Sqrt(math.Max(0, sumRG2/n-(sumRG/n)*(sumRG/n)))
	stdYB := math.Sqrt(math.Max(0, sumYB2/n-(sumYB/n)*(sumYB/n)))
	meanRG := sumRG / n
	meanYB := sumYB / n
	colorfulness := math.Sqrt(stdRG*stdRG+stdYB*stdYB) + 0.3*math.Sqrt(meanRG*meanRG+meanYB*meanYB)

	return Metrics{
		Dhash:        dHash(img),
		Width:        b.Dx(),
		Height:       b.Dy(),
		BlurVar:      laplacianVariance(gray, w, h),
		Brightness:   sumLuma / n,
		Colorfulness: colorfulness,
	}
}

// scaledRGBA downscales img to fit within maxSide (preserving aspect ratio).
func scaledRGBA(img image.Image, maxSide int) *image.RGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	nw, nh := w, h
	if w >= h {
		if w > maxSide {
			nw = maxSide
			nh = h * maxSide / w
		}
	} else if h > maxSide {
		nh = maxSide
		nw = w * maxSide / h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

// laplacianVariance applies a 3x3 Laplacian over the luma plane and returns the
// variance of the response. Sharp images have strong high-frequency edges (high
// variance); blurry ones do not.
func laplacianVariance(gray []float64, w, h int) float64 {
	if w < 3 || h < 3 {
		return 0
	}
	at := func(x, y int) float64 { return gray[y*w+x] }
	var sum, sumSq float64
	n := 0
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			lap := at(x-1, y) + at(x+1, y) + at(x, y-1) + at(x, y+1) - 4*at(x, y)
			sum += lap
			sumSq += lap * lap
			n++
		}
	}
	if n == 0 {
		return 0
	}
	mean := sum / float64(n)
	return sumSq/float64(n) - mean*mean
}

// dHash produces a 64-bit perceptual hash (row-wise gradient signature).
func dHash(img image.Image) uint64 {
	const w = 9
	const h = 8

	b := img.Bounds()
	dx := b.Dx()
	dy := b.Dy()
	if dx == 0 || dy == 0 {
		return 0
	}

	var lum [h][w]uint8
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			sx := b.Min.X + (x*dx)/w
			sy := b.Min.Y + (y*dy)/h
			r, gr, bl, _ := img.At(sx, sy).RGBA()
			rr := float64(r) / 65535.0
			gg := float64(gr) / 65535.0
			bb := float64(bl) / 65535.0
			yv := 0.299*rr + 0.587*gg + 0.114*bb
			lum[y][x] = uint8(yv * 255.0)
		}
	}

	var out uint64
	var bit uint
	for y := 0; y < h; y++ {
		for x := 0; x < w-1; x++ {
			if lum[y][x] > lum[y][x+1] {
				out |= 1 << bit
			}
			bit++
		}
	}
	return out
}
