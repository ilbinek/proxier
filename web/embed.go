// Package web embeds the built single-page app (web/dist) into the binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const notBuilt = `<!doctype html><meta charset="utf-8"><title>proxier</title>
<body style="font-family:system-ui;padding:2rem;max-width:40rem">
<h1>UI not built</h1>
<p>This binary was compiled without the web UI. Build with <code>task build</code> (or <code>pnpm --dir web install &amp;&amp; pnpm --dir web build</code>
then rebuild), or use a release binary. The API under <code>/api</code> is available.</p>`

// Handler serves static assets and falls back to index.html for client-side routes.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	index, idxErr := fs.ReadFile(sub, "index.html")
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if p != "" && p != "index.html" {
			if st, err := fs.Stat(sub, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					// Vite emits content-hashed file names.
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			if path.Ext(p) != "" {
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		if idxErr != nil {
			w.Write([]byte(notBuilt))
			return
		}
		w.Write(index)
	})
}
