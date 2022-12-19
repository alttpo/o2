//go:build debug

package main

import (
	"net/http"
)

func MaxAge(h http.Handler) http.Handler {
	return h
}
