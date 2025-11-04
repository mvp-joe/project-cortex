package storage

import (
	"encoding/binary"
	"fmt"
	"math"
)

// SerializeEmbedding converts a float32 slice to bytes using little-endian encoding.
// Each float32 is encoded as 4 bytes using IEEE 754 binary representation.
//
// For 384-dimension embeddings: 384 * 4 = 1536 bytes.
// For 1536-dimension embeddings: 1536 * 4 = 6144 bytes.
//
// The serialized format is used for storing embeddings in SQLite BLOB columns.
func SerializeEmbedding(emb []float32) []byte {
	bytes := make([]byte, len(emb)*4)
	for i, f := range emb {
		bits := math.Float32bits(f)
		binary.LittleEndian.PutUint32(bytes[i*4:], bits)
	}
	return bytes
}

// DeserializeEmbedding converts bytes back to a float32 slice using little-endian encoding.
// This reverses the serialization performed by SerializeEmbedding.
//
// Returns an error if the byte length is not divisible by 4, which indicates corrupted data.
// Empty byte slices are valid and return an empty (non-nil) float32 slice.
func DeserializeEmbedding(bytes []byte) ([]float32, error) {
	if len(bytes)%4 != 0 {
		return nil, fmt.Errorf("invalid embedding data: length %d not divisible by 4", len(bytes))
	}

	floats := make([]float32, len(bytes)/4)
	for i := range floats {
		bits := binary.LittleEndian.Uint32(bytes[i*4:])
		floats[i] = math.Float32frombits(bits)
	}
	return floats, nil
}
