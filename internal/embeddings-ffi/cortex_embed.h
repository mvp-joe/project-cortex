#ifndef CORTEX_EMBED_H
#define CORTEX_EMBED_H

#include <stdint.h>
#include <stdbool.h>
#include <stdlib.h>

typedef struct EmbeddingsHandle EmbeddingsHandle;

// Initialize embeddings model
EmbeddingsHandle* embeddings_init(const char* model_path, const char* tokenizer_path);

// Encode single text
bool embeddings_encode(
    const EmbeddingsHandle* handle,
    const char* text,
    float** embeddings_out,
    size_t* len_out
);

// Encode batch of texts
bool embeddings_encode_batch(
    const EmbeddingsHandle* handle,
    const char** texts,
    size_t num_texts,
    float** embeddings_out,
    size_t* len_out
);

// Free embeddings result
void embeddings_free_result(float* embeddings, size_t len);

// Free handle
void embeddings_free(EmbeddingsHandle* handle);

// Get embedding dimension
size_t embeddings_get_dimension(const EmbeddingsHandle* handle);

#endif
