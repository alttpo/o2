//go:build !debug

package dist

import "embed"

//go:embed index.html debug.html favicon.ico r
var Content embed.FS
