//go:build onnx

// Package analyze, onnx flavour. MobileNetV2-7 ImageNet classifier driven by
// yalue/onnxruntime_go (which dlopens libonnxruntime at run time).
//
// What you get: a fully-local, one-time pass that writes a useless-bucket label
// onto each image so the existing analyze pipeline can flag store/product/menu
// photos as "useless content". ImageNet's 1000 classes are not a perfect fit
// for "is this a personal photo I want to keep" — the keyword map below covers
// the obviously-useless classes. For semantic precision (CLIP zero-shot
// prompts), use ml/classify.py instead.
//
// This file is gated behind `-tags onnx` so the default binary stays small and
// has no native dependency.

package analyze

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-birds/photosift/internal/index"
	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	modelURL = "https://github.com/onnx/models/raw/main/validated/vision/" +
		"classification/mobilenet/model/mobilenetv2-7.onnx"
	labelsURL = "https://raw.githubusercontent.com/anishathalye/" +
		"imagenet-simple-labels/master/imagenet-simple-labels.json"
	inputSize = 224
)

// onnxUselessKeywords maps substrings of ImageNet labels to the shared useless
// buckets in classify.go. The mapping is intentionally conservative; an
// ambiguous label produces no label (treated as "photo").
var onnxUselessKeywords = map[string]string{
	"web site":   "document",
	"website":    "document",
	"menu":       "document",
	"book jacket": "document",
	"comic book": "document",
	"envelope":   "document",
	"packet":     "document",
	"carton":     "document",
	"binder":     "document",
	"notebook":   "document",
	"crossword":  "document",
	"monitor":    "screenshot",
	"screen":     "screenshot",
	"price tag":  "product in a store",
}

func labelToBucket(label string) string {
	low := strings.ToLower(label)
	for k, v := range onnxUselessKeywords {
		if strings.Contains(low, k) {
			return v
		}
	}
	return ""
}

// ClassifyWithONNX runs MobileNetV2 over every not-yet-labelled image.
func ClassifyWithONNX(db *sql.DB, opts ONNXOptions) (int, error) {
	if err := ensureAssets(&opts); err != nil {
		return 0, err
	}
	if err := initRuntime(opts.OnnxLib); err != nil {
		return 0, err
	}
	defer ort.DestroyEnvironment()

	labels, err := loadLabels(opts.LabelsPath)
	if err != nil {
		return 0, fmt.Errorf("load labels: %w", err)
	}

	imgs, err := index.LoadAllImages(db)
	if err != nil {
		return 0, err
	}

	session, inBuf, outTensor, err := newSession(opts.ModelPath)
	if err != nil {
		return 0, err
	}
	defer session.Destroy()

	results := make([]Label, 0, len(imgs))
	for _, img := range imgs {
		if img.ContentLabel != "" {
			continue
		}
		label, score, err := classifyImage(img.Path, inBuf, outTensor, session, labels)
		if err != nil {
			continue // skip failures, keep going
		}
		bucket := labelToBucket(label)
		if bucket == "" {
			bucket = "photo"
		}
		results = append(results, Label{Path: img.Path, Label: bucket, Score: score})
	}
	return ApplyLabels(db, results)
}

// newSession opens a reusable session bound to one input buffer and one output
// tensor so we can swap pixels in place instead of re-allocating per image.
func newSession(modelPath string) (*ort.AdvancedSession, []float32, *ort.Tensor[float32], error) {
	inBuf := make([]float32, 3*inputSize*inputSize)
	inTensor, err := ort.NewTensor(ort.NewShape(1, 3, inputSize, inputSize), inBuf)
	if err != nil {
		return nil, nil, nil, err
	}
	outTensor, err := ort.NewEmptyTensor[float32](ort.NewShape(1, 1000))
	if err != nil {
		return nil, nil, nil, err
	}
	session, err := ort.NewAdvancedSession(
		modelPath,
		[]string{"input"}, []string{"output"},
		[]ort.ArbitraryTensor{inTensor},
		[]ort.ArbitraryTensor{outTensor},
		nil,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	return session, inBuf, outTensor, nil
}

func classifyImage(path string, inBuf []float32, outTensor *ort.Tensor[float32],
	session *ort.AdvancedSession, labels []string,
) (string, float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", 0, err
	}
	preprocess(img, inBuf)
	if err := session.Run(); err != nil {
		return "", 0, err
	}
	output := outTensor.GetData()
	bestIdx, bestVal := 0, output[0]
	for i, v := range output {
		if v > bestVal {
			bestVal, bestIdx = v, i
		}
	}
	return labels[bestIdx], softmaxOne(output, bestIdx), nil
}

// preprocess writes the image into the shared input buffer in NCHW float32
// form, normalized with ImageNet mean/std.
func preprocess(img image.Image, dst []float32) {
	r := image.NewRGBA(image.Rect(0, 0, inputSize, inputSize))
	draw.ApproxBiLinear.Scale(r, r.Bounds(), img, img.Bounds(), draw.Src, nil)
	mean := [3]float32{0.485, 0.456, 0.406}
	std := [3]float32{0.229, 0.224, 0.225}
	plane := inputSize * inputSize
	for y := 0; y < inputSize; y++ {
		for x := 0; x < inputSize; x++ {
			i := r.PixOffset(x, y)
			rv := float32(r.Pix[i]) / 255
			gv := float32(r.Pix[i+1]) / 255
			bv := float32(r.Pix[i+2]) / 255
			off := y*inputSize + x
			dst[0*plane+off] = (rv - mean[0]) / std[0]
			dst[1*plane+off] = (gv - mean[1]) / std[1]
			dst[2*plane+off] = (bv - mean[2]) / std[2]
		}
	}
}

// softmaxOne returns the softmax probability of the index'th logit only — the
// only number we actually use downstream.
func softmaxOne(logits []float32, idx int) float64 {
	var max float32
	for _, v := range logits {
		if v > max {
			max = v
		}
	}
	var sum float64
	for _, v := range logits {
		sum += math.Exp(float64(v - max))
	}
	return math.Exp(float64(logits[idx]-max)) / sum
}

func ensureAssets(opts *ONNXOptions) error {
	if opts.ModelPath == "" {
		home, _ := os.UserHomeDir()
		opts.ModelPath = filepath.Join(home, ".photosift", "models", "mobilenetv2.onnx")
	}
	if opts.LabelsPath == "" {
		opts.LabelsPath = strings.TrimSuffix(opts.ModelPath, ".onnx") + "-labels.json"
	}
	if err := os.MkdirAll(filepath.Dir(opts.ModelPath), 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(opts.ModelPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "downloading MobileNetV2 model (~14MB)...")
		if err := download(modelURL, opts.ModelPath); err != nil {
			return fmt.Errorf("download model: %w", err)
		}
	}
	if _, err := os.Stat(opts.LabelsPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "downloading ImageNet labels...")
		if err := download(labelsURL, opts.LabelsPath); err != nil {
			return fmt.Errorf("download labels: %w", err)
		}
	}
	return nil
}

func download(url, dst string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

var runtimeReady bool

func initRuntime(libPath string) error {
	if runtimeReady {
		return nil
	}
	if libPath == "" {
		libPath = os.Getenv("PHOTOSIFT_ONNX_LIB")
	}
	if libPath != "" {
		ort.SetSharedLibraryPath(libPath)
	}
	if err := ort.InitializeEnvironment(); err != nil {
		return fmt.Errorf(
			"onnxruntime init failed (install libonnxruntime and set PHOTOSIFT_ONNX_LIB): %w",
			err,
		)
	}
	runtimeReady = true
	return nil
}

func loadLabels(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var labels []string
	if err := json.Unmarshal(b, &labels); err != nil {
		return nil, err
	}
	if len(labels) != 1000 {
		return nil, fmt.Errorf("expected 1000 labels, got %d", len(labels))
	}
	return labels, nil
}
