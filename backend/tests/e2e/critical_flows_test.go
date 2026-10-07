//go:build e2e

// Package e2e contiene pruebas End-to-End reales: se ejecutan con HTTP real
// contra un backend desplegado (API + Postgres + Redis + MinIO vía Docker
// Compose), nunca contra dobles en memoria. El paquete solo se compila con
// la build tag "e2e" (go test -tags=e2e ./tests/e2e/...) para no mezclarse
// con `go test ./...`, que sigue usando los casos unitarios normales.
//
// Requiere la variable de entorno E2E_BASE_URL apuntando al stack vivo
// (p.ej. http://localhost:8081 en el host del desarrollador, o
// http://nginx dentro de la red de Docker Compose — ver el servicio
// "e2e-tests" en docker-compose.yml).
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// Infraestructura mínima del cliente HTTP de pruebas
// ---------------------------------------------------------------------------

type apiClient struct {
	baseURL string
	http    *http.Client
	token   string
}

func newAPIClient(t *testing.T) *apiClient {
	base := os.Getenv("E2E_BASE_URL")
	if base == "" {
		base = "http://localhost:8081"
	}
	c := &apiClient{baseURL: base, http: &http.Client{Timeout: 15 * time.Second}}
	c.waitForHealth(t)
	return c
}

// waitForHealth evita condiciones de carrera contra el arranque del stack:
// reintenta /health hasta 30s antes de fallar, en vez de asumir que la API
// ya está lista apenas el proceso de pruebas inicia.
func (c *apiClient) waitForHealth(t *testing.T) {
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := c.http.Get(c.baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("la API en %s nunca respondió healthy (último error: %v)", c.baseURL, lastErr)
}

type apiResponse struct {
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
	Error   string          `json:"error"`
	Details []string        `json:"details"`
}

// do ejecuta una petición HTTP real contra el stack desplegado y decodifica
// el envelope {success, data, error, details} común a todos los handlers.
func (c *apiClient) do(t *testing.T, method, path string, body any, authenticated bool) (*http.Response, apiResponse) {
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("no se pudo serializar el payload para %s %s: %v", method, path, err)
		}
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reader)
	if err != nil {
		t.Fatalf("no se pudo construir la petición %s %s: %v", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authenticated && c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		t.Fatalf("la petición %s %s falló a nivel de transporte: %v", method, path, err)
	}
	defer resp.Body.Close()

	var parsed apiResponse
	if decErr := json.NewDecoder(resp.Body).Decode(&parsed); decErr != nil {
		t.Fatalf("respuesta no-JSON de %s %s (status %d): %v", method, path, resp.StatusCode, decErr)
	}
	return resp, parsed
}

// ---------------------------------------------------------------------------
// Suite de 9 flujos críticos, cada uno un sub-test independiente
// (ejecutables también de forma aislada con -run) pero que comparten
// estado via el closure de TestE2ECriticalFlows: el curso creado en el
// Flujo 5 es el que se publica en el 6 y se usa para inscripción/quiz en
// el 7/8/9, igual que lo haría un usuario real recorriendo la plataforma.
// ---------------------------------------------------------------------------

func TestE2ECriticalFlows(t *testing.T) {
	client := newAPIClient(t)

	// Identificadores únicos por ejecución para que la suite sea repetible
	// contra una base de datos persistente (sin depender de un reseteo
	// entre corridas).
	runID := uuid.New().String()[:8]
	studentEmail := fmt.Sprintf("e2e.%s@mooc.com", runID)
	const studentPassword = "Password123!"

	var (
		verificationCode string
		studentID        string
		courseID         string
		versionID        string
		unitID           string
		resourceID       string
		quizID           string
		attemptID        string
	)

	t.Run("01_health_check", func(t *testing.T) {
		resp, err := client.http.Get(client.baseURL + "/health")
		if err != nil {
			t.Fatalf("GET /health falló: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("esperaba 200 en /health, obtuve %d", resp.StatusCode)
		}
	})

	t.Run("02_registro_estudiante", func(t *testing.T) {
		resp, parsed := client.do(t, http.MethodPost, "/api/v1/auth/register", map[string]string{
			"email":     studentEmail,
			"password":  studentPassword,
			"full_name": "Estudiante E2E " + runID,
		}, false)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("registro falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}

		var payload struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
			VerificationCode string `json:"verification_code"`
		}
		if err := json.Unmarshal(parsed.Data, &payload); err != nil {
			t.Fatalf("no se pudo leer la respuesta de registro: %v", err)
		}
		if payload.VerificationCode == "" || payload.User.ID == "" {
			t.Fatalf("registro no devolvió verification_code o user.id: %+v", payload)
		}
		verificationCode = payload.VerificationCode
		studentID = payload.User.ID
	})

	t.Run("03_verificacion_email", func(t *testing.T) {
		resp, parsed := client.do(t, http.MethodPost, "/api/v1/auth/verify-email", map[string]string{
			"token": verificationCode,
		}, false)
		if resp.StatusCode != http.StatusOK || !parsed.Success {
			t.Fatalf("verify-email falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
	})

	t.Run("04_login_y_sesion", func(t *testing.T) {
		resp, parsed := client.do(t, http.MethodPost, "/api/v1/auth/login", map[string]string{
			"email":    studentEmail,
			"password": studentPassword,
		}, false)
		if resp.StatusCode != http.StatusOK || !parsed.Success {
			t.Fatalf("login falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}

		var payload struct {
			Session struct {
				Token string `json:"token"`
			} `json:"session"`
		}
		if err := json.Unmarshal(parsed.Data, &payload); err != nil {
			t.Fatalf("no se pudo leer la respuesta de login: %v", err)
		}
		if payload.Session.Token == "" {
			t.Fatalf("login no devolvió session.token: %+v", payload)
		}
		client.token = payload.Session.Token
	})

	t.Run("05_autoria_jerarquia_curso", func(t *testing.T) {
		// Crear curso. Sin cabecera X-Teacher-ID/X-Admin-ID ni usuario
		// autenticado con rol docente/admin, el handler usa como fallback
		// el admin sembrado por defecto (00000000-0000-0000-0000-000000000001),
		// que SeedDefaultAdmin garantiza que existe en cada arranque limpio.
		resp, parsed := client.do(t, http.MethodPost, "/api/v1/courses", map[string]any{
			"title":         "Curso E2E " + runID,
			"summary":       "Curso generado por la suite E2E",
			"passing_score": 60.0,
		}, false)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("creación de curso falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var created struct {
			Course struct {
				ID string `json:"id"`
			} `json:"course"`
			Version struct {
				ID string `json:"id"`
			} `json:"version"`
		}
		if err := json.Unmarshal(parsed.Data, &created); err != nil {
			t.Fatalf("no se pudo leer la respuesta de creación de curso: %v", err)
		}
		courseID = created.Course.ID
		versionID = created.Version.ID
		if courseID == "" || versionID == "" {
			t.Fatalf("creación de curso no devolvió course.id/version.id: %+v", created)
		}

		// Módulo
		resp, parsed = client.do(t, http.MethodPost, "/api/v1/courses/versions/"+versionID+"/modules", map[string]any{
			"title":       "Módulo 1",
			"description": "Módulo generado por E2E",
			"position":    1,
		}, false)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("creación de módulo falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var mod struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(parsed.Data, &mod); err != nil || mod.ID == "" {
			t.Fatalf("no se pudo leer el módulo creado: %v (%s)", err, string(parsed.Data))
		}

		// Unidad
		resp, parsed = client.do(t, http.MethodPost, "/api/v1/courses/modules/"+mod.ID+"/units", map[string]any{
			"title":    "Unidad 1",
			"position": 1,
		}, false)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("creación de unidad falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var unit struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(parsed.Data, &unit); err != nil || unit.ID == "" {
			t.Fatalf("no se pudo leer la unidad creada: %v (%s)", err, string(parsed.Data))
		}
		unitID = unit.ID

		// Recurso visible y obligatorio: condición mínima para que el
		// Flujo 06 (intento fallido de publicación) tenga algo que
		// rechazar por otra razón y el Flujo 07 (publicación exitosa)
		// tenga al menos un recurso visible que cumpla la validación.
		resp, parsed = client.do(t, http.MethodPost, "/api/v1/courses/units/"+unitID+"/resources", map[string]any{
			"title": "Lectura 1",
			// "text" es uno de los tipos permitidos por el CHECK de
			// course_resources.type en la base de datos (ver
			// migrations/000002_create_courses_tables.up.sql). Un tipo
			// fuera de esa lista ('article', por ejemplo) hace que el
			// INSERT viole el constraint y el handler devuelva 500 en
			// vez de un 400 de validación — ver nota en ARCHITECTURE.md
			// sobre esta brecha de validación de entrada.
			"type":               "text",
			"canonical_markdown": "# Contenido E2E",
			"is_visible":         true,
			"is_mandatory":       true,
			"position":           1,
		}, false)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("creación de recurso falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var res struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(parsed.Data, &res); err != nil || res.ID == "" {
			t.Fatalf("no se pudo leer el recurso creado: %v (%s)", err, string(parsed.Data))
		}
		resourceID = res.ID
	})

	t.Run("06_publicacion_rechazada_lista_exhaustiva", func(t *testing.T) {
		// Crea una SEGUNDA versión/curso deliberadamente incompleta (sin
		// módulos) para comprobar que PublishVersion devuelve la lista
		// EXHAUSTIVA de violaciones en "details", no solo la primera —
		// esto es exactamente lo que señaló el feedback del profesor
		// sobre la validación de publicación.
		resp, parsed := client.do(t, http.MethodPost, "/api/v1/courses", map[string]any{
			"title":         "Curso Incompleto E2E " + runID,
			"summary":       "",
			"passing_score": 0,
		}, false)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("creación de curso incompleto falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var created struct {
			Version struct {
				ID string `json:"id"`
			} `json:"version"`
		}
		if err := json.Unmarshal(parsed.Data, &created); err != nil {
			t.Fatalf("no se pudo leer el curso incompleto creado: %v", err)
		}

		resp, parsed = client.do(t, http.MethodPost, "/api/v1/courses/versions/"+created.Version.ID+"/publish", nil, false)
		if resp.StatusCode != http.StatusBadRequest || parsed.Success {
			t.Fatalf("esperaba rechazo 400 al publicar curso incompleto, obtuve status=%d success=%v", resp.StatusCode, parsed.Success)
		}
		if len(parsed.Details) < 2 {
			t.Fatalf("esperaba al menos 2 violaciones en 'details' (lista exhaustiva), obtuve %d: %v", len(parsed.Details), parsed.Details)
		}
	})

	t.Run("07_publicacion_exitosa_y_catalogo", func(t *testing.T) {
		resp, parsed := client.do(t, http.MethodPost, "/api/v1/courses/versions/"+versionID+"/publish", nil, false)
		if resp.StatusCode != http.StatusOK || !parsed.Success {
			t.Fatalf("publicación del curso válido falló: status=%d error=%q details=%v", resp.StatusCode, parsed.Error, parsed.Details)
		}

		// El curso publicado debe quedar visible en el catálogo público.
		resp, parsed = client.do(t, http.MethodGet, "/api/v1/courses/", nil, false)
		if resp.StatusCode != http.StatusOK || !parsed.Success {
			t.Fatalf("listado de catálogo falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var courses []struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(parsed.Data, &courses); err != nil {
			t.Fatalf("no se pudo leer el catálogo: %v", err)
		}
		found := false
		for _, c := range courses {
			if c.ID == courseID {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("el curso publicado %s no aparece en el catálogo público", courseID)
		}
	})

	t.Run("08_inscripcion_y_heartbeat", func(t *testing.T) {
		resp, parsed := client.do(t, http.MethodPost, "/api/v1/learning/enrollments", map[string]string{
			"student_id": studentID,
			"course_id":  courseID,
		}, true)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("inscripción falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var enrollment struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(parsed.Data, &enrollment); err != nil || enrollment.Status != "active" {
			t.Fatalf("inscripción no quedó activa: %v (%s)", err, string(parsed.Data))
		}

		resp, parsed = client.do(t, http.MethodPost, "/api/v1/learning/heartbeat", map[string]any{
			"student_id":            studentID,
			"course_id":             courseID,
			"resource_stable_id":    uuid.New().String(),
			"dwell_time_seconds":    30,
			"last_position_seconds": 30,
		}, true)
		if resp.StatusCode != http.StatusOK || !parsed.Success {
			t.Fatalf("heartbeat falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
	})

	t.Run("09_quiz_envio_y_rechazo_doble_envio", func(t *testing.T) {
		resp, parsed := client.do(t, http.MethodPost, "/api/v1/courses/resources/"+resourceID+"/quiz", map[string]any{
			"max_attempts":  3,
			"passing_score": 50.0,
			"questions": []map[string]any{
				{
					"question_text": "¿2 + 2?",
					"position":      1,
					"points":        100.0,
					"options": []map[string]any{
						{"option_text": "3", "is_correct": false, "position": 1},
						{"option_text": "4", "is_correct": true, "position": 2},
					},
				},
			},
		}, false)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("creación de quiz falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var quiz struct {
			ID        string `json:"id"`
			Questions []struct {
				ID      string `json:"id"`
				Options []struct {
					ID         string `json:"id"`
					OptionText string `json:"option_text"`
				} `json:"options"`
			} `json:"questions"`
		}
		if err := json.Unmarshal(parsed.Data, &quiz); err != nil || quiz.ID == "" || len(quiz.Questions) == 0 {
			t.Fatalf("no se pudo leer el quiz creado: %v (%s)", err, string(parsed.Data))
		}
		quizID = quiz.ID
		questionID := quiz.Questions[0].ID
		var correctOptionID string
		for _, opt := range quiz.Questions[0].Options {
			if opt.OptionText == "4" {
				correctOptionID = opt.ID
			}
		}
		if correctOptionID == "" {
			t.Fatalf("no se encontró la opción correcta en el quiz creado")
		}

		resp, parsed = client.do(t, http.MethodPost, "/api/v1/learning/quizzes/"+quizID+"/start-attempt", map[string]string{
			"student_id": studentID,
		}, true)
		if resp.StatusCode != http.StatusCreated || !parsed.Success {
			t.Fatalf("inicio de intento falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var attempt struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(parsed.Data, &attempt); err != nil || attempt.ID == "" {
			t.Fatalf("no se pudo leer el intento creado: %v (%s)", err, string(parsed.Data))
		}
		attemptID = attempt.ID

		answers := map[string]string{questionID: correctOptionID}

		resp, parsed = client.do(t, http.MethodPost, "/api/v1/learning/quizzes/"+quizID+"/attempts", map[string]any{
			"attempt_id": attemptID,
			"student_id": studentID,
			"answers":    answers,
		}, true)
		if resp.StatusCode != http.StatusOK || !parsed.Success {
			t.Fatalf("envío de intento falló: status=%d error=%q", resp.StatusCode, parsed.Error)
		}
		var submitted struct {
			Status string   `json:"status"`
			Score  *float64 `json:"score"`
		}
		if err := json.Unmarshal(parsed.Data, &submitted); err != nil {
			t.Fatalf("no se pudo leer el intento enviado: %v", err)
		}
		if submitted.Status != "submitted" && submitted.Status != "graded" {
			t.Fatalf("estado inesperado tras enviar el intento: %q", submitted.Status)
		}
		if submitted.Score == nil || *submitted.Score < 99.9 {
			t.Fatalf("puntaje inesperado para una respuesta 100%% correcta: %v", submitted.Score)
		}

		// Reenviar el MISMO intento debe ser rechazado (409) y NO debe
		// recalificar — es exactamente el caso que el feedback marcó
		// como ausente ("no hay... recuperación" de errores de flujo).
		resp, parsed = client.do(t, http.MethodPost, "/api/v1/learning/quizzes/"+quizID+"/attempts", map[string]any{
			"attempt_id": attemptID,
			"student_id": studentID,
			"answers":    answers,
		}, true)
		if resp.StatusCode != http.StatusConflict || parsed.Success {
			t.Fatalf("esperaba rechazo 409 al reenviar un intento ya enviado, obtuve status=%d success=%v error=%q", resp.StatusCode, parsed.Success, parsed.Error)
		}
	})
}
