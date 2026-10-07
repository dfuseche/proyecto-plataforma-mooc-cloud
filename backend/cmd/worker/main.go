package main

import (
	"context"
	"database/sql"
	"log"

	"github.com/hibiken/asynq"
	_ "github.com/lib/pq"
	"github.com/mooc-platform/backend/internal/config"
	"github.com/mooc-platform/backend/internal/course"
	"github.com/mooc-platform/backend/internal/media"
	"github.com/mooc-platform/backend/internal/worker"
)

func main() {
	cfg := config.Load()

	log.Println("[WORKER-DAEMON] Iniciando Worker de Procesamiento Asíncrono MOOC...")

	// Conexión a PostgreSQL
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Error al conectar con DB en Worker: %v", err)
	}
	defer func() {
		if cerr := db.Close(); cerr != nil {
			log.Printf("[WARNING] Error al cerrar la conexión a PostgreSQL: %v", cerr)
		}
	}()

	// Conexión a MinIO/S3
	storage, err := media.NewStorageService(cfg)
	if err != nil {
		log.Fatalf("Error al inicializar almacenamiento S3 en Worker: %v", err)
	}
	storage.EnsureBuckets(context.Background())

	courseRepo := course.NewPostgresRepository(db)
	processor := worker.NewProcessor(storage, courseRepo)

	// Configuración del servidor Asynq en Redis
	srv := asynq.NewServer(
		asynq.RedisClientOpt{Addr: cfg.RedisAddr},
		asynq.Config{
			Concurrency: 5,
			Queues: map[string]int{
				"critical": 6,
				"default":  3,
				"low":      1,
			},
			ErrorHandler: asynq.ErrorHandlerFunc(func(ctx context.Context, task *asynq.Task, err error) {
				retried, _ := asynq.GetRetryCount(ctx)
				maxRetry, _ := asynq.GetMaxRetry(ctx)
				if retried >= maxRetry {
					log.Printf("[ALERTA-DLQ] La tarea %s (ID: %s) falló tras %d reintentos y fue enviada a la Dead-Letter Queue. Error: %v",
						task.Type(), task.ResultWriter(), retried, err)
				} else {
					log.Printf("[WORKER-RETRY] Reintento %d/%d para la tarea %s: %v", retried+1, maxRetry, task.Type(), err)
				}
			}),
		},
	)

	mux := asynq.NewServeMux()
	mux.HandleFunc(worker.TypeMediaTranscodeHLS, processor.HandleMediaTranscodeHLS)
	mux.HandleFunc(worker.TypeAntimalwareScan, processor.HandleAntimalwareScan)

	log.Println("[WORKER-DAEMON] Worker escuchando en la cola de tareas Redis/Asynq")
	if err := srv.Run(mux); err != nil {
		log.Fatalf("Error al iniciar Asynq server: %v", err)
	}
}
