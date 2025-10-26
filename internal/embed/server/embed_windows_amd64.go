//go:build windows && amd64

package server

import (
	"embed"
	"io/fs"
)

//go:embed all:data/windows-amd64
var _dataRaw embed.FS

var Data, _ = fs.Sub(_dataRaw, "data/windows-amd64")
