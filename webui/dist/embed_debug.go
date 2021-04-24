//+build debug

package dist

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

type localFS struct {
	root string
}

func (l *localFS) Open(name string) (fs.File, error) {
	path := filepath.Join(l.root, name)
	return os.Open(path)
}

var Content fs.FS

func init() {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(file)
	Content = &localFS{root: root}
}
