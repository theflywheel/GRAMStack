package application

import (
	"embed"
	"net/http"
)

//go:embed web/*
var web embed.FS

func webHandler(name, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := web.ReadFile("web/" + name)
		if err != nil {
			http.Error(w, "Page unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", kind)
		w.Write(raw)
	}
}
