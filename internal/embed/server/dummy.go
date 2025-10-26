package server

//go:generate go run ./generate

import _ "embed"

//go:embed embedding_service.py
var EmbeddingScript string
