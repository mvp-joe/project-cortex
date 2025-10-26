//go:build darwin && amd64

package server

import (
	"embed"
	"io/fs"
)

//go:embed all:data/darwin-amd64
var _dataRaw embed.FS

var Data, _ = fs.Sub(_dataRaw, "data/darwin-amd64")
