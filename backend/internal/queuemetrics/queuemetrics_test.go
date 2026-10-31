package queuemetrics

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
)

type fakeSource struct {
	queues  []string
	infos   map[string]*asynq.QueueInfo
	listErr error
}

func (f fakeSource) Queues() ([]string, error) { return f.queues, f.listErr }

func (f fakeSource) GetQueueInfo(q string) (*asynq.QueueInfo, error) {
	info, ok := f.infos[q]
	if !ok {
		return nil, errors.New("queue not found")
	}
	return info, nil
}

func TestCollectSumsExistingQueuesAndSkipsMissingOnes(t *testing.T) {
	src := fakeSource{
		// "critical" nunca recibió tareas: no existe en Redis.
		queues: []string{"default", "low", "other"},
		infos: map[string]*asynq.QueueInfo{
			"default": {Pending: 3, Active: 2, Retry: 1, ProcessedTotal: 10, FailedTotal: 1, Latency: 40 * time.Second},
			"low":     {Pending: 1, Archived: 2, ProcessedTotal: 5, Latency: 90 * time.Second},
			"other":   {Pending: 100},
		},
	}

	s, err := Collect(src, []string{"critical", "default", "low"})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	want := Snapshot{Pending: 4, Active: 2, Retry: 1, Archived: 2, ProcessedTotal: 15, FailedTotal: 1, OldestPending: 90 * time.Second}
	if s != want {
		t.Fatalf("Collect = %+v, want %+v", s, want)
	}
	if s.Backlog() != 6 {
		t.Fatalf("Backlog = %d, want 6 (pending + active)", s.Backlog())
	}
}

func TestCollectPropagatesErrors(t *testing.T) {
	if _, err := Collect(fakeSource{listErr: errors.New("redis down")}, []string{"default"}); err == nil {
		t.Fatal("expected error when queues cannot be listed")
	}
	src := fakeSource{queues: []string{"default"}, infos: map[string]*asynq.QueueInfo{}}
	if _, err := Collect(src, []string{"default"}); err == nil {
		t.Fatal("expected error when a listed queue cannot be read")
	}
}

func TestBuildRequestHasOneGlobalGaugePerMetric(t *testing.T) {
	at := time.Date(2026, 10, 31, 15, 4, 5, 0, time.UTC)
	body, err := BuildRequest("my-project", Snapshot{Pending: 1, Active: 2, OldestPending: 1500 * time.Millisecond}, at)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}

	var req createTimeSeriesRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(req.TimeSeries) != 8 {
		t.Fatalf("got %d time series, want 8", len(req.TimeSeries))
	}

	byName := map[string]timeSeries{}
	for _, ts := range req.TimeSeries {
		if ts.Resource.Type != "global" || ts.Resource.Labels["project_id"] != "my-project" {
			t.Fatalf("unexpected resource %+v", ts.Resource)
		}
		if len(ts.Points) != 1 || ts.Points[0].Interval.EndTime != "2026-10-31T15:04:05Z" {
			t.Fatalf("unexpected points %+v", ts.Points)
		}
		byName[strings.TrimPrefix(ts.Metric.Type, MetricPrefix)] = ts
	}

	backlog := byName["backlog"].Points[0].Value
	if backlog.Int64Value == nil || *backlog.Int64Value != "3" {
		t.Fatalf("backlog value = %+v, want int64Value \"3\"", backlog)
	}
	age := byName["oldest_pending_age_seconds"].Points[0].Value
	if age.DoubleValue == nil || *age.DoubleValue != 1.5 {
		t.Fatalf("oldest_pending_age_seconds = %+v, want 1.5", age)
	}
}

// fakeGCP simula el servidor de metadatos y Cloud Monitoring.
func fakeGCP(t *testing.T, monitoringStatus int) (*Publisher, *int, *[]byte) {
	t.Helper()
	tokenRequests := 0
	var lastBody []byte

	mux := http.NewServeMux()
	mux.HandleFunc("/computeMetadata/v1/instance/service-accounts/default/token", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Metadata-Flavor") != "Google" {
			http.Error(w, "missing Metadata-Flavor", http.StatusForbidden)
			return
		}
		tokenRequests++
		_, _ = w.Write([]byte(`{"access_token":"tok-123","expires_in":3600,"token_type":"Bearer"}`))
	})
	mux.HandleFunc("/computeMetadata/v1/project/project-id", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("proyecto-de-prueba\n"))
	})
	mux.HandleFunc("/v3/projects/proyecto-de-prueba/timeSeries", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer tok-123" {
			http.Error(w, "bad request", http.StatusUnauthorized)
			return
		}
		lastBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(monitoringStatus)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	p := NewPublisher("")
	p.MetadataURL = srv.URL + "/computeMetadata/v1"
	p.MonitoringURL = srv.URL
	return p, &tokenRequests, &lastBody
}

func TestPublisherDiscoversProjectAndReusesToken(t *testing.T) {
	p, tokenRequests, lastBody := fakeGCP(t, http.StatusOK)
	ctx := context.Background()

	if err := p.DiscoverProject(ctx); err != nil {
		t.Fatalf("DiscoverProject: %v", err)
	}
	if p.Project != "proyecto-de-prueba" {
		t.Fatalf("Project = %q", p.Project)
	}

	for i := 0; i < 2; i++ {
		if err := p.Publish(ctx, Snapshot{Pending: 2}, time.Now()); err != nil {
			t.Fatalf("Publish #%d: %v", i+1, err)
		}
	}
	if *tokenRequests != 1 {
		t.Fatalf("token requested %d times, want 1 (cached)", *tokenRequests)
	}
	if !strings.Contains(string(*lastBody), MetricPrefix+"backlog") {
		t.Fatalf("published body does not contain the backlog metric: %s", *lastBody)
	}
}

func TestPublisherReportsMonitoringErrors(t *testing.T) {
	p, _, _ := fakeGCP(t, http.StatusForbidden)
	p.Project = "proyecto-de-prueba"

	err := p.Publish(context.Background(), Snapshot{}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("Publish error = %v, want one mentioning 403", err)
	}
}
