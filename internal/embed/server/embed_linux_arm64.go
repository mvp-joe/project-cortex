//go:build linux && arm64

package server

import (
	"embed"
	"io/fs"
)

//go:embed all:data/linux-arm64
var _dataRaw embed.FS

var Data, _ = fs.Sub(_dataRaw, "data/linux-arm64")
