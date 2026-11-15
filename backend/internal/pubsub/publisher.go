package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	gpubsub "cloud.google.com/go/pubsub"
)

// Publisher encapsula la publicación de mensajes en un topic de Cloud Pub/Sub.
type Publisher struct {
	client *gpubsub.Client
	topic  *gpubsub.Topic
	mu     sync.Mutex
}

// NewPublisher crea un publicador conectado a Cloud Pub/Sub para el proyecto y topic indicados.
func NewPublisher(ctx context.Context, projectID, topicID string) (*Publisher, error) {
	if projectID == "" {
		projectID = "proyecto-plataforma-mooc"
	}
	if topicID == "" {
		topicID = "mooc-tasks-topic"
	}

	client, err := gpubsub.NewClient(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("error al inicializar cliente de Pub/Sub: %w", err)
	}

	topic := client.Topic(topicID)
	// Verificar o asegurar que el topic existe en GCP (opcional en runtime)
	log.Printf("[PUBSUB-INIT] Publicador de Cloud Pub/Sub inicializado para proyecto=%s, topic=%s", projectID, topicID)

	return &Publisher{
		client: client,
		topic:  topic,
	}, nil
}

// Publish envía un mensaje con un payload serializable a JSON al topic de Pub/Sub.
// Retorna el MessageID asignado por Google Cloud Pub/Sub.
func (p *Publisher) Publish(ctx context.Context, payload interface{}, attributes map[string]string) (string, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("error serializando payload para Pub/Sub: %w", err)
	}

	msg := &gpubsub.Message{
		Data:       data,
		Attributes: attributes,
	}

	result := p.topic.Publish(ctx, msg)
	serverID, err := result.Get(ctx)
	if err != nil {
		return "", fmt.Errorf("fallo al publicar mensaje en Pub/Sub: %w", err)
	}

	log.Printf("[PUBSUB-PUBLISH] Mensaje publicado exitosamente en topic %s. Server MessageID: %s", p.topic.ID(), serverID)
	return serverID, nil
}

// Close libera los recursos del cliente y el topic.
func (p *Publisher) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.topic != nil {
		p.topic.Stop()
	}
	if p.client != nil {
		return p.client.Close()
	}
	return nil
}
