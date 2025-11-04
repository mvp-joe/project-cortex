package storage

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for Embedding Serialization
//
// The SerializeEmbedding and DeserializeEmbedding functions handle conversion between
// float32 slices and byte arrays using IEEE 754 little-endian encoding. This is critical
// for storing vector embeddings in SQLite and must be consistent across all storage operations.
//
// Test cases cover:
// 1. Round-trip conversion (serialize → deserialize → verify equality)
// 2. 384-dimension embeddings (production use case)
// 3. Special IEEE 754 values (NaN, Inf, -Inf, subnormal)
// 4. Byte order verification (little-endian)
// 5. Empty embeddings
// 6. Error cases (invalid byte lengths)
// 7. Performance benchmarks

func TestSerializeDeserialize_RoundTrip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		embedding []float32
	}{
		{
			name:      "small embedding",
			embedding: []float32{1.234, -5.678, 0.0, 999.999, -0.001},
		},
		{
			name:      "production 384-dim",
			embedding: makeTestEmbedding(384),
		},
		{
			name:      "single value",
			embedding: []float32{1.0},
		},
		{
			name:      "empty embedding",
			embedding: []float32{},
		},
		{
			name: "special float values",
			embedding: []float32{
				float32(math.NaN()),
				float32(math.Inf(1)),
				float32(math.Inf(-1)),
				0.0,
				-0.0,
				1.23e-38, // Near smallest positive normalized float32
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			serialized := SerializeEmbedding(tt.embedding)
			deserialized, err := DeserializeEmbedding(serialized)

			require.NoError(t, err)
			require.Equal(t, len(tt.embedding), len(deserialized))

			for i := range tt.embedding {
				// Use NaN-aware comparison
				if math.IsNaN(float64(tt.embedding[i])) {
					assert.True(t, math.IsNaN(float64(deserialized[i])))
				} else {
					assert.Equal(t, tt.embedding[i], deserialized[i])
				}
			}
		})
	}
}

func TestSerializeEmbedding_ByteLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		dimension    int
		expectedSize int
	}{
		{"empty", 0, 0},
		{"single", 1, 4},
		{"small", 5, 20},
		{"bge-small-en-v1.5", 384, 1536},
		{"text-embedding-3-small", 1536, 6144},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			embedding := makeTestEmbedding(tt.dimension)
			serialized := SerializeEmbedding(embedding)

			assert.Equal(t, tt.expectedSize, len(serialized))
		})
	}
}

func TestSerializeEmbedding_ByteOrder(t *testing.T) {
	t.Parallel()

	// Test specific value to verify little-endian byte order
	embedding := []float32{1.0}
	serialized := SerializeEmbedding(embedding)

	// IEEE 754 representation of 1.0: 0x3F800000
	// Little endian: [0x00, 0x00, 0x80, 0x3F]
	expected := []byte{0x00, 0x00, 0x80, 0x3F}
	assert.Equal(t, expected, serialized)
}

func TestDeserializeEmbedding_InvalidLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		bytes  []byte
		errMsg string
	}{
		{
			name:   "not divisible by 4",
			bytes:  []byte{0x00, 0x00, 0x80}, // 3 bytes
			errMsg: "invalid embedding data: length 3 not divisible by 4",
		},
		{
			name:   "single byte",
			bytes:  []byte{0xFF},
			errMsg: "invalid embedding data: length 1 not divisible by 4",
		},
		{
			name:   "two bytes",
			bytes:  []byte{0xFF, 0xFF},
			errMsg: "invalid embedding data: length 2 not divisible by 4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := DeserializeEmbedding(tt.bytes)

			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "invalid embedding data")
		})
	}
}

func TestDeserializeEmbedding_EmptyBytes(t *testing.T) {
	t.Parallel()

	result, err := DeserializeEmbedding([]byte{})
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NotNil(t, result) // Should be empty slice, not nil
}

func TestDeserializeEmbedding_NilBytes(t *testing.T) {
	t.Parallel()

	result, err := DeserializeEmbedding(nil)
	require.NoError(t, err)
	assert.Empty(t, result)
	assert.NotNil(t, result) // Should be empty slice, not nil
}

// Benchmark serialization performance
func BenchmarkEmbeddingSerialization(b *testing.B) {
	embedding := makeTestEmbedding(384)

	b.Run("serialize", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = SerializeEmbedding(embedding)
		}
	})

	b.Run("deserialize", func(b *testing.B) {
		serialized := SerializeEmbedding(embedding)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, _ = DeserializeEmbedding(serialized)
		}
	})

	b.Run("roundtrip", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			serialized := SerializeEmbedding(embedding)
			_, _ = DeserializeEmbedding(serialized)
		}
	})
}

// makeTestEmbedding creates a test embedding of the specified dimension.
func makeTestEmbedding(dim int) []float32 {
	emb := make([]float32, dim)
	for i := range emb {
		emb[i] = float32(i) * 0.001
	}
	return emb
}
