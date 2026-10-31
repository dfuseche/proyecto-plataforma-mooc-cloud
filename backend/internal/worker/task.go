package worker

import (
	"encoding/json"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
)

const (
	TypeMediaTranscodeHLS = "media:transcode_hls"
	TypeAntimalwareScan   = "media:antimalware_scan"
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

type AntimalwareScanPayload struct {
	ResourceID uuid.UUID `json:"resource_id"`
	ObjectKey  string    `json:"object_key"`
}

func NewAntimalwareScanTask(resourceID uuid.UUID, objectKey string) (*asynq.Task, error) {
	payload, err := json.Marshal(AntimalwareScanPayload{
		ResourceID: resourceID,
		ObjectKey:  objectKey,
	})
	if err != nil {
		return nil, err
	}

	return asynq.NewTask(
		TypeAntimalwareScan,
		payload,
		asynq.TaskID("scan:"+resourceID.String()),
		asynq.MaxRetry(3),
	), nil
}
