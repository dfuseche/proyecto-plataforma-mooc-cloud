# Stage 0: frontend (React + Vite). El contexto de build es la raíz del repo.
FROM node:22-alpine AS web

WORKDIR /web

COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund --ignore-scripts

COPY frontend/ ./
# Versión visible en el pie de la SPA (commit que generó la imagen).
ARG APP_VERSION=dev
ENV VITE_APP_VERSION=$APP_VERSION
RUN npm run build

# Stage 1: Build
FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git gcc musl-dev

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .

RUN CGO_ENABLED=0 GOOS=linux go build -o /app/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/queue-metrics ./cmd/queue-metrics

# Stage 2: Run
FROM alpine:3.19

WORKDIR /app

RUN apk add --no-cache ca-certificates ffmpeg curl

# Commit con el que se construyó la imagen: la API lo devuelve en X-App-Version.
ARG APP_VERSION=dev
ENV APP_VERSION=$APP_VERSION

COPY --from=builder /app/api /app/api
COPY --from=builder /app/worker /app/worker
# Exportador de métricas de la cola (corre en la VM de Redis, ver deploy/scripts/queue-metrics.sh).
COPY --from=builder /app/queue-metrics /app/queue-metrics
COPY --from=builder /app/migrations /app/migrations
COPY --from=builder /app/docs /app/docs
# La API sirve la SPA desde WEB_DIR (por defecto /app/web).
COPY --from=web /web/dist /app/web

# El proceso no necesita privilegios: se ejecuta con un usuario sin shell ni root.
RUN addgroup -S app && adduser -S -G app -u 10001 app
USER app

EXPOSE 8080

CMD ["/app/api"]
