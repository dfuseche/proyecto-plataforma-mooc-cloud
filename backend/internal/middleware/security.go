package middleware

import (
	"net/http"
	"os"
	"strings"
)

// mediaHosts son los orígenes de almacenamiento a los que el navegador puede
// llegar directamente (URLs prefirmadas de subida y descarga, segmentos HLS).
// Se pueden ampliar con CSP_MEDIA_HOSTS (separados por espacios).
//
// Cloud Storage entrega las URLs prefirmadas en estilo "virtual host"
// (https://<bucket>.storage.googleapis.com/...), por eso se permite también
// el comodín de subdominios además del dominio base.
func mediaHosts() string {
	hosts := "https://storage.googleapis.com https://*.storage.googleapis.com"
	if extra := strings.TrimSpace(os.Getenv("CSP_MEDIA_HOSTS")); extra != "" {
		hosts += " " + extra
	}
	return hosts
}

var contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: " + mediaHosts() + "; " +
	"media-src 'self' blob: " + mediaHosts() + "; " +
	"connect-src 'self' " + mediaHosts() + "; " +
	"font-src 'self'; worker-src 'self' blob:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

		next.ServeHTTP(w, r)
	})
}
