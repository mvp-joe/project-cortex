//go:build linux && amd64

package server

import (
	"embed"
	"io/fs"
)

//go:embed all:data/linux-amd64
var _dataRaw embed.FS

var Data, _ = fs.Sub(_dataRaw, "data/linux-amd64")
