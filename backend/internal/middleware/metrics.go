package middleware

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Metrics acumula contadores básicos de peticiones HTTP (total y por
// código de estado) y la suma/cantidad de duraciones, para exponerlos en
// formato de texto de Prometheus desde GET /metrics.
//
// Se implementa a mano, sin el cliente oficial de Prometheus
// (github.com/prometheus/client_golang), porque añadir una dependencia
// nueva requeriría regenerar go.sum con el toolchain de Go, que no está
// disponible en este entorno de edición; el formato de texto expuesto
// aquí es el mismo que ese cliente produciría y Prometheus lo scrapea
// igual. El objetivo es cerrar la brecha señalada en el feedback
// ("no hay... métricas"), no sustituir una librería estándar si el
// equipo decide añadirla más adelante.
type Metrics struct {
	mu          sync.Mutex
	requests    map[requestKey]int64
	durationSum map[requestKey]float64
	startedAt   time.Time
}

type requestKey struct {
	method string
	path   string
	status int
}

func NewMetrics() *Metrics {
	return &Metrics{
		requests:    make(map[requestKey]int64),
		durationSum: make(map[requestKey]float64),
		startedAt:   time.Now(),
	}
}

// idSegment detecta UUIDs e IDs numéricos en la ruta para normalizarlos a
// ":id". Sin esto, cada curso/recurso/intento distinto generaría su propia
// serie de métricas (cardinalidad no acotada, creciendo para siempre).
var idSegment = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$|^\d+$`)

func normalizePath(path string) string {
	segments := splitPath(path)
	for i, seg := range segments {
		if idSegment.MatchString(seg) {
			segments[i] = ":id"
		}
	}
	return "/" + joinPath(segments)
}

func splitPath(path string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '/' {
			if i > start {
				out = append(out, path[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func joinPath(segments []string) string {
	out := ""
	for i, s := range segments {
		if i > 0 {
			out += "/"
		}
		out += s
	}
	return out
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (sr *statusRecorder) WriteHeader(code int) {
	sr.status = code
	sr.ResponseWriter.WriteHeader(code)
}

// Middleware registra cada petición completada (método, ruta normalizada,
// código de estado y duración). Se coloca después de chi.middleware.Logger
// en la cadena: el logger ya deja constancia legible por humano en los
// logs de cada request (con su X-Request-ID), y este middleware agrega
// los mismos datos en forma de contadores consultables.
func (m *Metrics) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sr, r)

		key := requestKey{
			method: r.Method,
			path:   normalizePath(r.URL.Path),
			status: sr.status,
		}
		elapsed := time.Since(start).Seconds()

		m.mu.Lock()
		m.requests[key]++
		m.durationSum[key] += elapsed
		m.mu.Unlock()
	})
}

// Handler expone los contadores acumulados en el formato de texto de
// Prometheus (text/plain; version=0.0.4), listo para que un scraper los
// consuma sin autenticación (son agregados, no contienen datos de
// usuarios).
func (m *Metrics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		defer m.mu.Unlock()

		keys := make([]requestKey, 0, len(m.requests))
		for k := range m.requests {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if keys[i].path != keys[j].path {
				return keys[i].path < keys[j].path
			}
			if keys[i].method != keys[j].method {
				return keys[i].method < keys[j].method
			}
			return keys[i].status < keys[j].status
		})

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		fmt.Fprintf(w, "# HELP process_uptime_seconds Tiempo transcurrido desde que el proceso empezó a servir tráfico.\n")
		fmt.Fprintf(w, "# TYPE process_uptime_seconds gauge\n")
		fmt.Fprintf(w, "process_uptime_seconds %.3f\n", time.Since(m.startedAt).Seconds())

		fmt.Fprintf(w, "# HELP http_requests_total Número total de peticiones HTTP procesadas, por método, ruta y código de estado.\n")
		fmt.Fprintf(w, "# TYPE http_requests_total counter\n")
		for _, k := range keys {
			fmt.Fprintf(w, "http_requests_total{method=%q,path=%q,status=%q} %d\n",
				k.method, k.path, strconv.Itoa(k.status), m.requests[k])
		}

		fmt.Fprintf(w, "# HELP http_request_duration_seconds_sum Suma acumulada de la duración de las peticiones, en segundos, por método, ruta y código de estado.\n")
		fmt.Fprintf(w, "# TYPE http_request_duration_seconds_sum counter\n")
		for _, k := range keys {
			fmt.Fprintf(w, "http_request_duration_seconds_sum{method=%q,path=%q,status=%q} %.6f\n",
				k.method, k.path, strconv.Itoa(k.status), m.durationSum[k])
		}

		fmt.Fprintf(w, "# HELP http_requests_errors_total Número total de peticiones HTTP con código de estado >= 500, por método y ruta.\n")
		fmt.Fprintf(w, "# TYPE http_requests_errors_total counter\n")
		errorTotals := make(map[string]int64)
		errorOrder := make([]string, 0)
		for _, k := range keys {
			if k.status < 500 {
				continue
			}
			ek := k.method + " " + k.path
			if _, seen := errorTotals[ek]; !seen {
				errorOrder = append(errorOrder, ek)
			}
			errorTotals[ek] += m.requests[k]
		}
		sort.Strings(errorOrder)
		for _, ek := range errorOrder {
			var method, path string
			fmt.Sscanf(ek, "%s %s", &method, &path)
			fmt.Fprintf(w, "http_requests_errors_total{method=%q,path=%q} %d\n", method, path, errorTotals[ek])
		}
	}
}
