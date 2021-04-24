//+build !debug

package dist

import "embed"

//go:embed index.html favicon.ico r
var Content embed.FS
