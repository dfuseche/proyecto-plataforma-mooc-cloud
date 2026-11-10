package main

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	_ "github.com/lib/pq"
	"github.com/mooc-platform/backend/internal/config"
	"github.com/mooc-platform/backend/internal/course"
	"github.com/mooc-platform/backend/internal/media"
	"github.com/mooc-platform/backend/internal/worker"
)

// PubSubPushEnvelope representa el formato estándar de mensajes Push que envía Cloud Pub/Sub
type PubSubPushEnvelope struct {
	Message struct {
		Attributes  map[string]string `json:"attributes"`
		Data        string            `json:"data"` // String en Base64
		MessageID   string            `json:"messageId"`
		PublishTime string            `json:"publishTime"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

func main() {
	cfg := config.Load()

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Println("[WORKER-HTTP] Iniciando Servicio Worker de Cloud Run para tareas asíncronas...")
	log.Printf("[WORKER-HTTP] Escuchando en puerto %s", port)

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

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(15 * time.Minute)

	// Conexión a Cloud Storage / MinIO S3
	storage, err := media.NewStorageService(cfg)
	if err != nil {
		log.Fatalf("Error al inicializar almacenamiento en Worker: %v", err)
	}
	storage.EnsureBuckets(context.Background())

	courseRepo := course.NewPostgresRepository(db)
	processor := worker.NewProcessor(storage, courseRepo)

	mux := http.NewServeMux()

	// Healthcheck para Cloud Run
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok","service":"mooc-worker"}`))
	})

	// Endpoint receptor de eventos Push de Cloud Pub/Sub
	mux.HandleFunc("/pubsub/transcode", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Método no permitido", http.StatusMethodNotAllowed)
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("[WORKER-PUSH-ERROR] Error leyendo cuerpo de la solicitud: %v", err)
			http.Error(w, "Error leyendo cuerpo", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		var envelope PubSubPushEnvelope
		if err := json.Unmarshal(bodyBytes, &envelope); err != nil {
			log.Printf("[WORKER-PUSH-ERROR] Error deserializando sobre de Pub/Sub: %v", err)
			http.Error(w, "JSON inválido", http.StatusBadRequest)
			return
		}

		msgID := envelope.Message.MessageID
		log.Printf("[WORKER-PUSH-RECEIVED] Mensaje recibido de Pub/Sub (MessageID: %s, Subscription: %s)", msgID, envelope.Subscription)

		// Decodificar payload Base64
		dataBytes, err := base64.StdEncoding.DecodeString(envelope.Message.Data)
		if err != nil {
			log.Printf("[WORKER-PUSH-ERROR] Error decodificando base64 del MessageID %s: %v", msgID, err)
			http.Error(w, "Base64 inválido", http.StatusBadRequest)
			return
		}

		var payload worker.MediaTranscodeHLSPayload
		if err := json.Unmarshal(dataBytes, &payload); err != nil {
			log.Printf("[WORKER-PUSH-ERROR] Payload inválido para transcodificación (MessageID %s): %v", msgID, err)
			// Retornamos 200 para descartar mensajes corruptos irrecuperables
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ignored_corrupt_payload"}`))
			return
		}

		log.Printf("[WORKER-TASK-START] Iniciando tarea para recurso=%s, key=%s, media_type=%s (MessageID: %s)",
			payload.ResourceID, payload.OriginalKey, payload.MediaType, msgID)

		// Procesar transcodificación HLS con idempotencia integrada
		if err := processor.ProcessMediaTranscode(r.Context(), payload); err != nil {
			log.Printf("[WORKER-TASK-ERROR] Falló procesamiento de transcodificación (MessageID: %s, recurso: %s): %v",
				msgID, payload.ResourceID, err)
			// Retornar 500 (NACK) para que Pub/Sub reintente la entrega según la política de reintentos
			http.Error(w, fmt.Sprintf("Error procesando transcodificación: %v", err), http.StatusInternalServerError)
			return
		}

		// Confirmación exitosa (ACK)
		log.Printf("[WORKER-TASK-SUCCESS] Transcodificación completada y confirmada con ACK (MessageID: %s, recurso: %s)",
			msgID, payload.ResourceID)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"status":"ack","message_id":"%s","resource_id":"%s"}`, msgID, payload.ResourceID)))
	})

	// Servidor HTTP con timeouts adecuados para Cloud Run
	server := &http.Server{
		Addr:              fmt.Sprintf(":%s", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("[WORKER-READY] Worker HTTP listo y esperando eventos Push en /pubsub/transcode en el puerto %s", port)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Error en servidor HTTP del worker: %v", err)
	}
}
