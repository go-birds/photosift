package analyze

import "errors"

// ONNXOptions configures the optional Go-native ML classifier. The actual
// implementation lives in ml_onnx.go and is gated behind `-tags onnx`; the
// default build's ClassifyWithONNX (ml_stub.go) just reports that.
type ONNXOptions struct {
	ModelPath  string // path to the .onnx model; auto-downloaded if missing
	LabelsPath string // path to the labels file; auto-downloaded if missing
	OnnxLib    string // libonnxruntime path; PHOTOSIFT_ONNX_LIB used if empty
}

// ErrONNXNotCompiled is the sentinel ClassifyWithONNX returns when the build
// wasn't compiled with `-tags onnx`. Callers can branch on this to print a
// rebuild hint without leaking the error text everywhere.
var ErrONNXNotCompiled = errors.New(
	"ONNX support not compiled in; rebuild with 'go build -tags onnx ./cmd/photosift' and install libonnxruntime",
)
