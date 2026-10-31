// Exportador de métricas de la cola Asynq a Cloud Monitoring.
//
// Corre como contenedor en la VM de Redis, fuera de los grupos que escalan
// (ver deploy/scripts/queue-metrics.sh). Cada QUEUE_METRICS_INTERVAL publica
// custom.googleapis.com/mooc/asynq/backlog (tareas pendientes + activas), que
// es la señal de autoscaling del grupo de workers, junto con el resto del
// estado de la cola (paquete internal/queuemetrics). Suma las mismas colas que
// consume el worker (worker.QueuePriorities) y publica en el proyecto de la
// VM, que lee del servidor de metadatos.
//
// Variables de entorno:
//
//	REDIS_ADDR              dirección de Redis (default localhost:6379)
//	QUEUE_METRICS_INTERVAL  periodo de muestreo (default 15s, mínimo 10s)
//	QUEUE_METRICS_DRY_RUN   "true" = solo escribe las muestras en el log, no publica (fuera de GCP)
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/mooc-platform/backend/internal/config"
	"github.com/mooc-platform/backend/internal/queuemetrics"
	"github.com/mooc-platform/backend/internal/worker"
)

// Cloud Monitoring acepta como máximo un punto cada 5 s por serie; 10 s deja margen.
const minInterval = 10 * time.Second

func main() {
	if err := run(); err != nil {
		log.Fatalf("[QUEUE-METRICS] %v", err)
	}
}

func run() error {
	cfg := config.Load()
	interval := durationEnv("QUEUE_METRICS_INTERVAL", 15*time.Second)
	if interval < minInterval {
		interval = minInterval
	}
	dryRun := strings.EqualFold(os.Getenv("QUEUE_METRICS_DRY_RUN"), "true")
	queues := workerQueues()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	inspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: cfg.RedisAddr})
	defer func() {
		if err := inspector.Close(); err != nil {
			log.Printf("[WARNING] Error al cerrar el inspector de Asynq: %v", err)
		}
	}()

	var pub *queuemetrics.Publisher
	if !dryRun {
		pub = queuemetrics.NewPublisher("")
		if err := pub.DiscoverProject(ctx); err != nil {
			return fmt.Errorf("no se pudo leer el proyecto del servidor de metadatos (fuera de GCP usa QUEUE_METRICS_DRY_RUN=true): %w", err)
		}
	}

	// #nosec G706 -- intervalo y modo vienen de la configuración del despliegue, no de entrada de usuario
	log.Printf("[QUEUE-METRICS] Iniciado: colas=%v intervalo=%s publicar=%t", queues, interval, pub != nil)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		sample(ctx, inspector, queues, pub)
		select {
		case <-ctx.Done():
			log.Println("[QUEUE-METRICS] Detenido")
			return nil
		case <-ticker.C:
		}
	}
}

// sample toma una muestra, la escribe en el log y, si hay publicador, la
// envía a Cloud Monitoring. Un fallo no detiene el exportador: se reintenta
// en la siguiente muestra.
func sample(ctx context.Context, src queuemetrics.QueueSource, queues []string, pub *queuemetrics.Publisher) {
	snap, err := queuemetrics.Collect(src, queues)
	if err != nil {
		log.Printf("[QUEUE-METRICS-ERROR] %v", err)
		return
	}
	log.Printf("[QUEUE-METRICS] %s", snap)
	if pub == nil {
		return
	}
	if err := pub.Publish(ctx, snap, time.Now()); err != nil {
		log.Printf("[QUEUE-METRICS-ERROR] Publicando en Cloud Monitoring: %v", err)
	}
}

// workerQueues devuelve, en orden, las colas que consume el worker.
func workerQueues() []string {
	queues := make([]string, 0, len(worker.QueuePriorities))
	for q := range worker.QueuePriorities {
		queues = append(queues, q)
	}
	sort.Strings(queues)
	return queues
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		// #nosec G706 -- el valor es configuración del despliegue, no entrada de usuario
		log.Printf("[WARNING] %s=%q no es una duración válida; se usa %s", key, v, fallback)
		return fallback
	}
	return d
}
