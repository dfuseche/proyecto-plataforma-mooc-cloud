package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newSite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"index.html":        "<html>INDEX</html>",
		"assets/app-abc.js": "console.log(1)",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestSPAHandler(t *testing.T) {
	h := NewSPAHandler(newSite(t))
	if h == nil {
		t.Fatal("se esperaba un handler")
	}

	cases := []struct {
		name, method, path string
		code               int
		body, cache        string
	}{
		{"raíz entrega index", http.MethodGet, "/", http.StatusOK, "INDEX", "no-cache"},
		{"ruta del cliente cae en index", http.MethodGet, "/cursos/123", http.StatusOK, "INDEX", "no-cache"},
		{"asset con hash es cacheable", http.MethodGet, "/assets/app-abc.js", http.StatusOK, "console.log", "immutable"},
		{"asset inexistente cae en index", http.MethodGet, "/assets/nope.js", http.StatusOK, "INDEX", "no-cache"},
		{"ruta de API desconocida es 404", http.MethodGet, "/api/v1/nada", http.StatusNotFound, "", ""},
		{"POST no lo atiende la SPA", http.MethodPost, "/", http.StatusNotFound, "", ""},
		{"recorrido de directorios se rechaza", http.MethodGet, "/../../etc/passwd", http.StatusBadRequest, "", ""},
		{"recorrido codificado se rechaza", http.MethodGet, "/%2e%2e/%2e%2e/etc/passwd", http.StatusBadRequest, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(c.method, c.path, nil))
			if rec.Code != c.code {
				t.Fatalf("código %d, se esperaba %d", rec.Code, c.code)
			}
			if !strings.Contains(rec.Body.String(), c.body) {
				t.Fatalf("cuerpo %q no contiene %q", rec.Body.String(), c.body)
			}
			if c.cache != "" && !strings.Contains(rec.Header().Get("Cache-Control"), c.cache) {
				t.Fatalf("Cache-Control %q no contiene %q", rec.Header().Get("Cache-Control"), c.cache)
			}
		})
	}
}

func TestSPAHandlerSinFrontend(t *testing.T) {
	if NewSPAHandler(t.TempDir()) != nil {
		t.Fatal("sin index.html no debe registrarse nada")
	}
}
