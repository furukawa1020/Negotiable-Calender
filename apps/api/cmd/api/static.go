package main

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func withStaticFiles(next http.Handler, root string) http.Handler {
	if strings.TrimSpace(root) == "" {
		return next
	}
	fileServer := http.FileServer(http.Dir(root))
	indexPath := filepath.Join(root, "index.html")
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			next.ServeHTTP(response, request)
			return
		}
		if strings.HasPrefix(request.URL.Path, "/api/") || request.URL.Path == "/healthz" || request.URL.Path == "/readyz" {
			next.ServeHTTP(response, request)
			return
		}
		cleanPath := path.Clean("/" + request.URL.Path)
		relativePath := strings.TrimPrefix(cleanPath, "/")
		if relativePath != "" {
			fullPath := filepath.Join(root, filepath.FromSlash(relativePath))
			if relative, err := filepath.Rel(root, fullPath); err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
					fileServer.ServeHTTP(response, request)
					return
				}
			}
		}
		if path.Ext(cleanPath) != "" {
			http.NotFound(response, request)
			return
		}
		http.ServeFile(response, request, indexPath)
	})
}
