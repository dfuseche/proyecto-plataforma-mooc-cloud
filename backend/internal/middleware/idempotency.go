package middleware

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       bytes.Buffer
}

func (r *responseRecorder) WriteHeader(statusCode int) {
	r.statusCode = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func Idempotency(rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet || r.Method == http.MethodOptions || rdb == nil {
				next.ServeHTTP(w, r)
				return
			}

			idempotencyKey := r.Header.Get("Idempotency-Key")
			if idempotencyKey == "" {
				next.ServeHTTP(w, r)
				return
			}

			redisKey := fmt.Sprintf("idempotency:%s", idempotencyKey)
			ctx := r.Context()

			cachedVal, err := rdb.Get(ctx, redisKey).Result()
			if err == nil && cachedVal != "" {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Cache-Lookup", "HIT-IDEMPOTENT")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(cachedVal))
				return
			}

			rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)

			if rec.statusCode >= 200 && rec.statusCode < 300 {
				rdb.Set(ctx, redisKey, rec.body.String(), 24*time.Hour)
			}
		})
	}
}
