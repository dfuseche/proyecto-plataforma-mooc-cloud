package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/hibiken/asynq"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/mooc-platform/backend/internal/config"
	"github.com/mooc-platform/backend/internal/course"
	"github.com/mooc-platform/backend/internal/learning"
	"github.com/mooc-platform/backend/internal/media"
	internalMw "github.com/mooc-platform/backend/internal/middleware"
	"github.com/mooc-platform/backend/internal/user"
)

func runMigrations(db *sql.DB) error {
	migrationDirs := []string{"./migrations", "/app/migrations"}
	var targetDir string
	for _, dir := range migrationDirs {
		if _, err := os.Stat(dir); err == nil {
			targetDir = dir
			break
		}
	}

	if targetDir == "" {
		log.Println("[MIGRATIONS] Advertencia: No se encontró la carpeta de migraciones. Omitiendo auto-migración.")
		return nil
	}

	entries, err := os.ReadDir(targetDir)
	if err != nil {
		return err
	}

	var upFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".up.sql") {
			upFiles = append(upFiles, filepath.Join(targetDir, entry.Name()))
		}
	}

	sort.Strings(upFiles)

	for _, file := range upFiles {
		log.Printf("[MIGRATIONS] Ejecutando migración SQL: %s", filepath.Base(file))
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		if _, err := db.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", file, err)
		}
	}

	log.Println("[MIGRATIONS] Todas las migraciones SQL fueron aplicadas exitosamente.")
	return nil
}

func registerDocsRoutes(r chi.Router) {
	r.Get("/docs", func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "/docs/", http.StatusMovedPermanently)
	})

	r.Get("/docs/", func(w http.ResponseWriter, req *http.Request) {
		nonceBytes := make([]byte, 16)
		if _, err := rand.Read(nonceBytes); err != nil {
			http.Error(w, "Unable to initialize documentation", http.StatusInternalServerError)
			return
		}
		nonce := base64.RawStdEncoding.EncodeToString(nonceBytes)
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' https://unpkg.com 'nonce-"+nonce+"'; style-src 'self' https://unpkg.com; img-src 'self' data: blob:; connect-src 'self';")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>MOOC API - Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.11.10/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.10/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5.11.10/swagger-ui-standalone-preset.js"></script>
	<script nonce="` + nonce + `">
    window.onload = () => SwaggerUIBundle({
      url: "/openapi.yaml",
      dom_id: "#swagger-ui",
      deepLinking: true,
      presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
      layout: "StandaloneLayout"
    });
  </script>
</body>
</html>`
		_, _ = w.Write([]byte(html))
	})

	r.Get("/openapi.yaml", func(w http.ResponseWriter, req *http.Request) {
		content, err := os.ReadFile("./docs/openapi.yaml")
		if err != nil {
			http.Error(w, "OpenAPI specification not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		_, _ = w.Write(content)
	})
}

func main() {
	cfg := config.Load()

	// Conectar a PostgreSQL
	db, err := sql.Open("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Error al conectar con PostgreSQL: %v", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(15 * time.Minute)

	// Ejecutar migraciones automáticas
	if err := runMigrations(db); err != nil {
		log.Fatalf("Error ejecutando migraciones: %v", err)
	}

	// Conectar a Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: cfg.RedisAddr,
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Printf("[WARNING] No se pudo hacer ping a Redis (%s): %v", cfg.RedisAddr, err)
	}

	// Cliente Asynq para encolar tareas asíncronas
	asynqClient := asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.RedisAddr})
	defer asynqClient.Close()

	// El Inspector le permite al handler de media revisar/limpiar tareas
	// archivadas cuando un Enqueue choca por Task ID duplicado (ver
	// media.HTTPHandler.enqueueTranscodeTask).
	asynqInspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: cfg.RedisAddr})
	defer asynqInspector.Close()

	// Inicializar Servicio de Almacenamiento MinIO/S3
	storageService, err := media.NewStorageService(cfg)
	if err != nil {
		log.Printf("[WARNING] No se pudo conectar con MinIO S3 inmediatamente: %v", err)
	}

	// Inicializar capas de Dominio
	userRepo := user.NewPostgresRepository(db)
	userUC := user.NewUseCase(userRepo)
	userHandler := user.NewHTTPHandler(userUC)

	// Seed de Administrador Principal Inicial
	if err := userUC.SeedDefaultAdmin(context.Background()); err != nil {
		log.Printf("[WARNING] Error al sembrar admin inicial: %v", err)
	}

	courseRepo := course.NewPostgresRepository(db)
	courseUC := course.NewUseCase(courseRepo, userRepo, storageService)
	courseHandler := course.NewHTTPHandler(courseUC)

	learningRepo := learning.NewPostgresRepository(db)
	learningUC := learning.NewUseCase(learningRepo, courseRepo, userRepo, fmt.Sprintf("http://localhost:%s", cfg.Port))
	learningHandler := learning.NewHTTPHandler(learningUC)

	mediaHandler := media.NewHTTPHandler(storageService, courseRepo, learningRepo, asynqClient, asynqInspector)

	authMw := internalMw.NewAuthMiddleware(userRepo)
	rateLimiter := internalMw.NewRateLimiter(rdb, 100, 1*time.Minute)
	metrics := internalMw.NewMetrics()

	// Router Chi
	r := chi.NewRouter()

	// Middlewares globales
	r.Use(internalMw.SecurityHeaders)
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	// Metrics va después de Recoverer (para no perderse si un handler
	// hace panic) y antes de Idempotency/Authenticate, así cuenta TODAS
	// las peticiones que llegan al router, incluidas las rechazadas por
	// autenticación o limitadas por rate limit.
	r.Use(metrics.Middleware)
	r.Use(internalMw.Idempotency(rdb))
	r.Use(authMw.Authenticate)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "Idempotency-Key", "X-Admin-ID", "X-Teacher-ID"},
		ExposedHeaders:   []string{"Link", "ETag"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	registerDocsRoutes(r)

	// Rate limiter aplicado a autenticación
	r.With(rateLimiter.Middleware).Group(func(r chi.Router) {
		userHandler.RegisterRoutes(r)
	})

	// Endpoint Healthcheck
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok", "timestamp":"` + time.Now().Format(time.RFC3339) + `"}`))
	})

	// Métricas básicas en formato Prometheus (contadores de peticiones,
	// duración acumulada y errores 5xx, por método+ruta normalizada).
	// Ver docs/OPERACIONES.md, sección "Observabilidad", para cómo
	// correlacionar un request_id de un log con una fila de estas
	// métricas y para un ejemplo de regla de alertas.
	r.Get("/metrics", metrics.Handler())

	// Registrar rutas de dominios
	courseHandler.RegisterRoutes(r)
	if storageService != nil {
		mediaHandler.RegisterRoutes(r)
	}
	learningHandler.RegisterRoutes(r)

	serverAddr := fmt.Sprintf(":%s", cfg.Port)
	log.Printf("Servidor API de la Plataforma MOOC iniciado en %s", serverAddr)
	if err := http.ListenAndServe(serverAddr, r); err != nil {
		log.Fatalf("Error al iniciar el servidor HTTP: %v", err)
	}
}
