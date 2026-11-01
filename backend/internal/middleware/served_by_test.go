package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestServedBySetsInstanceAndVersionHeaders(t *testing.T) {
	handler := ServedBy("mooc-api-mig-abcd", "8a09ca5")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusTeapot {
		t.Fatalf("status = %d, the wrapped handler must still run", rec.Code)
	}
	if got := rec.Header().Get("X-Served-By"); got != "mooc-api-mig-abcd" {
		t.Fatalf("X-Served-By = %q", got)
	}
	if got := rec.Header().Get("X-App-Version"); got != "8a09ca5" {
		t.Fatalf("X-App-Version = %q", got)
	}
}

func TestInstanceNamePrefersEnvAndFallsBackToHostname(t *testing.T) {
	t.Setenv("INSTANCE_NAME", "mooc-worker-mig-x1y2")
	if got := InstanceName(); got != "mooc-worker-mig-x1y2" {
		t.Fatalf("InstanceName() = %q, want the INSTANCE_NAME value", got)
	}

	t.Setenv("INSTANCE_NAME", "")
	host, _ := os.Hostname()
	if got := InstanceName(); host != "" && got != host {
		t.Fatalf("InstanceName() = %q, want hostname %q", got, host)
	}
}

func TestAppVersionDefaultsToDev(t *testing.T) {
	t.Setenv("APP_VERSION", "")
	if got := AppVersion(); got != "dev" {
		t.Fatalf("AppVersion() = %q, want dev", got)
	}
	t.Setenv("APP_VERSION", "abc1234")
	if got := AppVersion(); got != "abc1234" {
		t.Fatalf("AppVersion() = %q, want abc1234", got)
	}
}
