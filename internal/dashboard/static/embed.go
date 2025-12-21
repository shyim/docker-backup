package static

import "embed"

//go:embed *.js *.css *.png
var Files embed.FS
