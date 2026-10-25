// Package web sirve la SPA de React compilada desde el mismo binario de la API,
// de modo que el navegador usa un único origen para la interfaz y /api.
package web

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// NewSPAHandler devuelve un handler para rutas no-API: entrega el archivo
// estático si existe y, si no, index.html (fallback de enrutamiento del lado
// del cliente). Devuelve nil si dir no existe, para no registrar nada en
// entornos sin frontend (tests, desarrollo solo-API).
func NewSPAHandler(dir string) http.Handler {
	index := filepath.Join(dir, "index.html")
	if _, err := os.Stat(index); err != nil {
		return nil
	}
	files := http.FileServer(http.Dir(dir))
	// os.DirFS rechaza por construcción rutas con ".." o absolutas, así que una
	// ruta de la petición nunca sale de dir.
	fsys := os.DirFS(dir)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.NotFound(w, r)
			return
		}
		// Rutas de API desconocidas: 404 JSON-agnóstico, no la SPA.
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if info, err := fs.Stat(fsys, name); name != "" && err == nil && !info.IsDir() {
			// Los assets de Vite llevan hash en el nombre: cacheables a largo plazo.
			if strings.HasPrefix(r.URL.Path, "/assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			}
			files.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, index)
	})
}
