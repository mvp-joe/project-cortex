//go:build darwin && arm64

package server

import (
	"embed"
	"io/fs"
)

//go:embed all:data/darwin-arm64
var _dataRaw embed.FS

// Data is the embedded Python packages with the platform directory stripped
var Data, _ = fs.Sub(_dataRaw, "data/darwin-arm64")
