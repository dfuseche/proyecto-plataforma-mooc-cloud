package middleware

import (
	"net/http"
	"os"
	"strings"
)

// ServedBy agrega a cada respuesta la instancia que la atendió (X-Served-By)
// y la versión de la imagen (X-App-Version). Con varias réplicas detrás del
// balanceador, permite comprobar desde el cliente (k6, curl) que una réplica
// nueva ya atiende tráfico y que todas ejecutan la misma versión. El nombre es
// el de la VM, ya visible en la consola de GCP: no es un dato sensible.
func ServedBy(instance, version string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Served-By", instance)
			w.Header().Set("X-App-Version", version)
			next.ServeHTTP(w, r)
		})
	}
}

// InstanceName devuelve INSTANCE_NAME (el nombre de la VM, que pasa
// deploy/startup/startup.sh) o, si no está definida, el hostname: dentro de
// Docker es el ID del contenedor, que igual es distinto en cada instancia.
func InstanceName() string {
	if name := strings.TrimSpace(os.Getenv("INSTANCE_NAME")); name != "" {
		return name
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "unknown"
}

// AppVersion devuelve APP_VERSION (el commit con el que se construyó la
// imagen, ver backend/Dockerfile) o "dev" si no está definida.
func AppVersion() string {
	if v := strings.TrimSpace(os.Getenv("APP_VERSION")); v != "" {
		return v
	}
	return "dev"
}
