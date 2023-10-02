//go:build !debug

package main

import (
	"fmt"
	"net/http"
	"path/filepath"
	"time"
)

func MaxAge(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var age time.Duration
		ext := filepath.Ext(r.URL.String())

		switch ext {
		case ".css", ".js":
			age = (time.Hour * 24 * 30) / time.Second
		case ".jpg", ".jpeg", ".gif", ".png", ".ico", ".cur", ".gz", ".svg", ".svgz",
			".ttf", ".otf",
			".mp4", ".ogg", ".ogv", ".webm", ".htc":
			age = (time.Hour * 24 * 365) / time.Second
		default:
			age = 0
		}

		if age > 0 {
			w.Header().Add("Cache-Control", fmt.Sprintf("max-age=%d, public, must-revalidate, proxy-revalidate", age))
		}

		h.ServeHTTP(w, r)
	})
}
