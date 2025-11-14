//go:build rust_ffi

package embeddingsffi

// #cgo darwin LDFLAGS: -lembeddings_ffi -framework Security -framework CoreFoundation
// #cgo linux LDFLAGS: -lembeddings_ffi -lunwind -lpthread -ldl -lm -static-libgcc
// #cgo windows LDFLAGS: -lembeddings_ffi -lws2_32 -luserenv -lbcrypt -lntdll
// #include "cortex_embed.h"
import "C"
import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"unsafe"
)

// Model wraps the embeddings model handle
type Model struct {
	handle *C.EmbeddingsHandle
	dim    int
}

// NewModel loads the ONNX model and tokenizer
func NewModel(modelPath, tokenizerPath string) (*Model, error) {
	cModelPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cModelPath))

	cTokenizerPath := C.CString(tokenizerPath)
	defer C.free(unsafe.Pointer(cTokenizerPath))

	handle := C.embeddings_init(cModelPath, cTokenizerPath)
	if handle == nil {
		return nil, errors.New("failed to initialize embeddings model")
	}

	dim := int(C.embeddings_get_dimension(handle))

	m := &Model{
		handle: handle,
		dim:    dim,
	}

	runtime.SetFinalizer(m, (*Model).Close)

	return m, nil
}

// Encode converts text to embeddings
func (m *Model) Encode(text string) ([]float32, error) {
	if m.handle == nil {
		return nil, errors.New("model is closed")
	}

	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))

	var embPtr *C.float
	var length C.size_t

	success := C.embeddings_encode(m.handle, cText, &embPtr, &length)
	if !success {
		return nil, errors.New("encoding failed")
	}
	defer C.embeddings_free_result(embPtr, length)

	// Copy to Go slice (manual conversion from C.float to float32)
	embeddings := make([]float32, length)
	if length > 0 {
		cSlice := unsafe.Slice(embPtr, length)
		for i, v := range cSlice {
			embeddings[i] = float32(v)
		}
	}

	return embeddings, nil
}

// EncodeBatch converts multiple texts to embeddings
func (m *Model) EncodeBatch(texts []string) ([][]float32, error) {
	if m.handle == nil {
		return nil, errors.New("model is closed")
	}

	if len(texts) == 0 {
		return nil, nil
	}

	// Convert Go strings to C strings
	cTexts := make([]*C.char, len(texts))
	for i, text := range texts {
		cTexts[i] = C.CString(text)
		defer C.free(unsafe.Pointer(cTexts[i]))
	}

	var embPtr *C.float
	var length C.size_t

	// Debug: log before calling Rust
	preview := texts[0]
	if len(preview) > 50 {
		preview = preview[:50]
	}
	fmt.Fprintf(os.Stderr, "[GO] Calling Rust FFI with %d texts, first text: %q\n", len(texts), preview)

	success := C.embeddings_encode_batch(
		m.handle,
		(**C.char)(unsafe.Pointer(&cTexts[0])),
		C.size_t(len(texts)),
		&embPtr,
		&length,
	)

	// Debug: log result
	fmt.Fprintf(os.Stderr, "[GO] Rust FFI returned success=%v, length=%d\n", success, length)

	if !success {
		return nil, errors.New("batch encoding failed")
	}
	defer C.embeddings_free_result(embPtr, length)

	// Copy to Go slice and reshape (manual conversion from C.float to float32)
	flatEmbeddings := make([]float32, length)
	if length > 0 {
		cSlice := unsafe.Slice(embPtr, length)
		for i, v := range cSlice {
			flatEmbeddings[i] = float32(v)
		}
	}

	// Reshape into batch
	result := make([][]float32, len(texts))
	for i := 0; i < len(texts); i++ {
		start := i * m.dim
		end := start + m.dim
		result[i] = flatEmbeddings[start:end]
	}

	return result, nil
}

// Dimensions returns the embedding dimension
func (m *Model) Dimensions() int {
	return m.dim
}

// Close frees the model resources
func (m *Model) Close() error {
	if m.handle != nil {
		C.embeddings_free(m.handle)
		m.handle = nil
		runtime.SetFinalizer(m, nil)
	}
	return nil
}
