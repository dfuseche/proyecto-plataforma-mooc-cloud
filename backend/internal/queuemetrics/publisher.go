package queuemetrics

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MetricPrefix es el prefijo de las métricas en Cloud Monitoring. La política
// de autoscaling del grupo de workers usa MetricPrefix + "backlog"
// (scripts/gcp/config.ps1).
const MetricPrefix = "custom.googleapis.com/mooc/asynq/"

const (
	defaultMonitoringURL = "https://monitoring.googleapis.com"
	defaultMetadataURL   = "http://metadata.google.internal/computeMetadata/v1"
)

// Publisher escribe las muestras en Cloud Monitoring con la API REST
// (projects.timeSeries.create), autenticado con la cuenta de servicio de la VM
// a través del servidor de metadatos, igual que deploy/startup/startup.sh. Así
// no hace falta agregar el cliente oficial de Google Cloud a go.mod.
type Publisher struct {
	Project       string
	MonitoringURL string
	MetadataURL   string
	Client        *http.Client

	token       string
	tokenExpiry time.Time
}

// NewPublisher crea un Publisher contra los endpoints reales de GCP.
func NewPublisher(project string) *Publisher {
	return &Publisher{
		Project:       project,
		MonitoringURL: defaultMonitoringURL,
		MetadataURL:   defaultMetadataURL,
		Client:        &http.Client{Timeout: 10 * time.Second},
	}
}

// DiscoverProject lee el ID del proyecto del servidor de metadatos de la VM.
func (p *Publisher) DiscoverProject(ctx context.Context) error {
	body, err := p.metadata(ctx, "project/project-id")
	if err != nil {
		return err
	}
	p.Project = strings.TrimSpace(string(body))
	if p.Project == "" {
		return errors.New("metadata server returned an empty project id")
	}
	return nil
}

// Publish escribe una muestra de cada métrica con el instante at.
func (p *Publisher) Publish(ctx context.Context, s Snapshot, at time.Time) error {
	body, err := BuildRequest(p.Project, s, at)
	if err != nil {
		return err
	}
	token, err := p.accessToken(ctx)
	if err != nil {
		return err
	}

	endpoint := p.MonitoringURL + "/v3/projects/" + url.PathEscape(p.Project) + "/timeSeries"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building Cloud Monitoring request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.Client.Do(req)
	if err != nil {
		return fmt.Errorf("posting to Cloud Monitoring: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("cloud monitoring returned %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
	}
	return nil
}

type metadataToken struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}

// accessToken devuelve el token de la cuenta de servicio de la VM y lo
// reutiliza hasta un minuto antes de que venza.
func (p *Publisher) accessToken(ctx context.Context) (string, error) {
	if p.token != "" && time.Now().Before(p.tokenExpiry) {
		return p.token, nil
	}
	body, err := p.metadata(ctx, "instance/service-accounts/default/token")
	if err != nil {
		return "", err
	}
	var tok metadataToken
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", fmt.Errorf("parsing metadata token: %w", err)
	}
	if tok.AccessToken == "" {
		return "", errors.New("metadata server returned an empty access token")
	}
	p.token = tok.AccessToken
	p.tokenExpiry = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - time.Minute)
	return p.token, nil
}

func (p *Publisher) metadata(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.MetadataURL+"/"+path, nil)
	if err != nil {
		return nil, fmt.Errorf("building metadata request: %w", err)
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("querying metadata server (%s): %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata server returned %d for %s", resp.StatusCode, path)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<10))
}

// Cuerpo de projects.timeSeries.create (solo los campos que se usan).
type createTimeSeriesRequest struct {
	TimeSeries []timeSeries `json:"timeSeries"`
}

type timeSeries struct {
	Metric   metricType        `json:"metric"`
	Resource monitoredResource `json:"resource"`
	Points   []point           `json:"points"`
}

type metricType struct {
	Type string `json:"type"`
}

type monitoredResource struct {
	Type   string            `json:"type"`
	Labels map[string]string `json:"labels"`
}

type point struct {
	Interval timeInterval `json:"interval"`
	Value    typedValue   `json:"value"`
}

type timeInterval struct {
	EndTime string `json:"endTime"`
}

// typedValue lleva un solo campo: int64Value va como string (mapeo JSON de
// int64 en la API de Google) y doubleValue como número.
type typedValue struct {
	Int64Value  *string  `json:"int64Value,omitempty"`
	DoubleValue *float64 `json:"doubleValue,omitempty"`
}

// BuildRequest arma el cuerpo de projects.timeSeries.create con una serie por
// métrica. Todas son GAUGE sobre el recurso "global": describen a la cola
// entera y no a una VM, que es lo que el autoscaler espera de una métrica por
// grupo (single-instance-assignment).
func BuildRequest(project string, s Snapshot, at time.Time) ([]byte, error) {
	res := monitoredResource{Type: "global", Labels: map[string]string{"project_id": project}}
	end := timeInterval{EndTime: at.UTC().Format(time.RFC3339Nano)}

	ints := []struct {
		name  string
		value int
	}{
		{"backlog", s.Backlog()},
		{"pending", s.Pending},
		{"active", s.Active},
		{"retry", s.Retry},
		{"archived", s.Archived},
		{"processed_total", s.ProcessedTotal},
		{"failed_total", s.FailedTotal},
	}

	req := createTimeSeriesRequest{TimeSeries: make([]timeSeries, 0, len(ints)+1)}
	for _, m := range ints {
		v := strconv.Itoa(m.value)
		req.TimeSeries = append(req.TimeSeries, timeSeries{
			Metric:   metricType{Type: MetricPrefix + m.name},
			Resource: res,
			Points:   []point{{Interval: end, Value: typedValue{Int64Value: &v}}},
		})
	}
	age := s.OldestPending.Seconds()
	req.TimeSeries = append(req.TimeSeries, timeSeries{
		Metric:   metricType{Type: MetricPrefix + "oldest_pending_age_seconds"},
		Resource: res,
		Points:   []point{{Interval: end, Value: typedValue{DoubleValue: &age}}},
	})

	return json.Marshal(req)
}
