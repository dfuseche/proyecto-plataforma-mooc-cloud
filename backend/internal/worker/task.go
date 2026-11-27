package worker

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const (
	TypeMediaTranscodeHLS = "media:transcode_hls"
)

// QueuePriorities son las colas que consume el worker y su prioridad relativa
// (cmd/worker). El exportador de métricas (cmd/queue-metrics) suma estas
// mismas colas para calcular el backlog que usa el autoscaling de workers.
var QueuePriorities = map[string]int{
	"critical": 6,
	"default":  3,
	"low":      1,
}

type MediaTranscodeHLSPayload struct {
	ResourceID  uuid.UUID `json:"resource_id"`
	OriginalKey string    `json:"original_key"`
	MediaType   string    `json:"media_type"` // video, audio
}

func NewMediaTranscodeHLSTask(resourceID uuid.UUID, originalKey string, mediaType string) (*asynq.Task, error) {
	payload, err := json.Marshal(MediaTranscodeHLSPayload{
		ResourceID:  resourceID,
		OriginalKey: originalKey,
		MediaType:   mediaType,
	})
	if err != nil {
		return nil, err
	}

	// Idempotencia utilizando el ID del recurso como Task ID único
	return asynq.NewTask(
		TypeMediaTranscodeHLS,
		payload,
		asynq.TaskID(resourceID.String()),
		asynq.MaxRetry(3), // Máximo 3 reintentos antes de enviar a DLQ (Sección 6)
	), nil
}
