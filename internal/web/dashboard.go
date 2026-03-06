package web

import (
	"embed"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

// DashboardHandler serves the embedded web dashboard.
func DashboardHandler() http.Handler {
	return http.FileServer(http.FS(staticFiles))
}
