package config

import (
	"os"
	"strconv"
)

type Config struct {
	Port                  string
	DatabaseURL           string
	RedisAddr             string
	MinIOEndpoint         string
	ExternalMinIOEndpoint string
	MinIOAccessKey        string
	MinIOSecretKey        string
	MinIOSecure            bool
	S3Region              string
	MediaBucket           string
	BadgeBucket           string
	MailpitHost           string
	MailpitPort           int
	JWTSecret             string
	WorkerConcurrency     int
	GCPProjectID          string
	PubSubTopicID         string
	CORSAllowedOrigins    string
}

func Load() *Config {
	return &Config{
		Port:                  getEnv("PORT", "8080"),
		DatabaseURL:           getEnv("DATABASE_URL", "postgres://mooc_user:j0zrNNl+1@localhost:5432/mooc_db?sslmode=disable"),
		RedisAddr:             getEnv("REDIS_ADDR", "localhost:6379"),
		MinIOEndpoint:         getEnv("MINIO_ENDPOINT", "localhost:9000"),
		ExternalMinIOEndpoint: getEnv("EXTERNAL_MINIO_ENDPOINT", ""),
		MinIOAccessKey:        getEnv("MINIO_ACCESS_KEY", "minioadmin"),
		MinIOSecretKey:        getEnv("MINIO_SECRET_KEY", "minioadminpassword"),
		MinIOSecure:           getEnvAsBool("MINIO_SECURE", false),
		S3Region:              getEnv("S3_REGION", getEnv("MINIO_REGION", "us-central1")),
		MediaBucket:           getEnv("S3_MEDIA_BUCKET", getEnv("MINIO_MEDIA_BUCKET", "mooc-media")),
		BadgeBucket:           getEnv("S3_BADGE_BUCKET", getEnv("MINIO_BADGE_BUCKET", "mooc-badges")),
		MailpitHost:           getEnv("MAILPIT_HOST", "localhost"),
		MailpitPort:           getEnvAsInt("MAILPIT_PORT", 1025),
		JWTSecret:             getEnv("JWT_SECRET", "super-secret-key-change-in-production"),
		// Tareas asynq o worker concurrency en paralelo
		WorkerConcurrency:     getEnvAsInt("WORKER_CONCURRENCY", 2),
		GCPProjectID:          getEnv("GCP_PROJECT", getEnv("GOOGLE_CLOUD_PROJECT", "proyecto-plataforma-mooc")),
		PubSubTopicID:         getEnv("PUBSUB_TOPIC", getEnv("TOPIC_ID", "mooc-tasks-topic")),
		CORSAllowedOrigins:    getEnv("CORS_ALLOWED_ORIGINS", ""),
	}
}

func getEnv(key, fallback string) string {
	if val, ok := os.LookupEnv(key); ok && val != "" {
		return val
	}
	return fallback
}

func getEnvAsBool(key string, fallback bool) bool {
	valStr := getEnv(key, "")
	if val, err := strconv.ParseBool(valStr); err == nil {
		return val
	}
	return fallback
}

func getEnvAsInt(key string, fallback int) int {
	valStr := getEnv(key, "")
	if val, err := strconv.Atoi(valStr); err == nil {
		return val
	}
	return fallback
}
