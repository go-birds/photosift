//go:build !onnx

package analyze

import "database/sql"

// ClassifyWithONNX is the stub used in the default (un-tagged) build. The
// real implementation in ml_onnx.go is compiled in only with `-tags onnx`.
func ClassifyWithONNX(db *sql.DB, opts ONNXOptions) (int, error) {
	return 0, ErrONNXNotCompiled
}
