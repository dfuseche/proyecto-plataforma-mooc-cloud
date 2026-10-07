package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	rdb        *redis.Client
	limit      int
	window     time.Duration
}

func NewRateLimiter(rdb *redis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		rdb:    rdb,
		limit:  limit,
		window: window,
	}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rl.rdb == nil {
			next.ServeHTTP(w, r)
			return
		}

		clientIP := r.RemoteAddr
		key := fmt.Sprintf("rate_limit:%s", clientIP)
		ctx := r.Context()

		count, err := rl.rdb.Incr(ctx, key).Result()
		if err != nil {
			// En caso de fallo de Redis, fallar de manera segura permitiendo el tráfico
			next.ServeHTTP(w, r)
			return
		}

		if count == 1 {
			rl.rdb.Expire(ctx, key, rl.window)
		}

		w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rl.limit))
		w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, rl.limit-int(count))))

		if int(count) > rl.limit {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(rl.window.Seconds())))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"success":false,"error":"Límite de tasa excedido (Rate limit exceeded). Intente más tarde."}`))
			return
		}

		next.ServeHTTP(w, r)
	})
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
