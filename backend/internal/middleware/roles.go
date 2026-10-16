package middleware

import (
	"net/http"
	"strings"

	"github.com/mooc-platform/backend/internal/domain"
)

// AccessPolicy centraliza qué rutas exigen sesión y qué rol. Va justo después
// de Authenticate (que deja pasar las peticiones anónimas) y concentra la
// política en un único sitio, en vez de repartirla por cada RegisterRoutes.
//
//   - /api/v1/admin/*                      -> admin
//   - escrituras y borradores de /courses  -> docente o admin
//   - /api/v1/media (subida, descarga por clave) -> docente o admin
//   - /api/v1/learning/*                   -> cualquier usuario autenticado
//
// Todo lo demás (registro/login, catálogo, jerarquía publicada, verificación
// de insignias, lectura de media firmada, health, métricas y la SPA) sigue
// siendo público.
func (m *AuthMiddleware) AccessPolicy(next http.Handler) http.Handler {
	teacherOrAdmin := m.RequireRole(domain.RoleTeacher, domain.RoleAdmin)(next)
	adminOnly := m.RequireRole(domain.RoleAdmin)(next)
	anyUser := m.RequireRole(domain.RoleStudent, domain.RoleTeacher, domain.RoleAdmin)(next)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/api/v1/admin/"):
			adminOnly.ServeHTTP(w, r)
		case strings.HasPrefix(p, "/api/v1/learning/"):
			anyUser.ServeHTTP(w, r)
		case strings.HasPrefix(p, "/api/v1/courses"):
			if isCoursePublicRead(r) {
				next.ServeHTTP(w, r)
				return
			}
			teacherOrAdmin.ServeHTTP(w, r)
		case p == "/api/v1/media/presigned-upload",
			p == "/api/v1/media/presigned-download",
			strings.HasSuffix(p, "/complete-upload"):
			teacherOrAdmin.ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// isCoursePublicRead: el catálogo y la jerarquía publicada son públicos; el
// resto de /courses (previews de borrador, mutaciones) es de docentes.
func isCoursePublicRead(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	p := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/courses"), "/")
	if p == "" {
		return true // GET /api/v1/courses
	}
	return strings.Count(p, "/") == 1 && !strings.HasSuffix(p, "/preview") // GET /courses/{id}
}
