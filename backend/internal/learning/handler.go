package learning

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/mooc-platform/backend/internal/domain"
	"github.com/mooc-platform/backend/internal/middleware"
)

type HTTPHandler struct {
	useCase *UseCase
}

func NewHTTPHandler(uc *UseCase) *HTTPHandler {
	return &HTTPHandler{useCase: uc}
}

func (h *HTTPHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/learning", func(r chi.Router) {
		r.Post("/enrollments", h.EnrollStudent)
		r.Get("/quizzes/{quizId}/snapshot", h.GetQuizSnapshot)
		r.Post("/quizzes/{quizId}/start-attempt", h.StartQuizAttempt)
		r.Patch("/attempts/{attemptId}", h.SavePartialAttempt)
		r.Post("/quizzes/{quizId}/attempts", h.SubmitQuizAttempt)
		r.Post("/attempts/{attemptId}/submit", h.SubmitQuizAttempt)
		r.Post("/heartbeat", h.RecordHeartbeat)
	})

	r.Post("/api/v1/courses/resources/{resourceId}/quiz", h.CreateQuiz)

	r.Route("/api/v1/badges", func(r chi.Router) {
		r.Get("/verify/{code}", h.VerifyBadge)
	})

	r.Post("/api/v1/admin/badges/{badgeId}/revoke", h.RevokeBadge)
}

type APIResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{Success: true, Data: data})
}

func respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{Success: false, Error: message})
}

func (h *HTTPHandler) EnrollStudent(w http.ResponseWriter, r *http.Request) {
	authUser := middleware.GetUserFromContext(r.Context())
	studentID := uuid.Nil
	if authUser != nil {
		studentID = authUser.ID
	}

	var req struct {
		StudentID string `json:"student_id"`
		CourseID  string `json:"course_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	if studentID == uuid.Nil && req.StudentID != "" {
		studentID, _ = uuid.Parse(req.StudentID)
	}

	courseID, err := uuid.Parse(req.CourseID)
	if err != nil || studentID == uuid.Nil {
		respondError(w, http.StatusBadRequest, "IDs de estudiante o curso inválidos")
		return
	}

	enrollment, err := h.useCase.EnrollStudent(r.Context(), studentID, courseID)
	if err != nil {
		if err == domain.ErrCourseNotFound {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, enrollment)
}

func (h *HTTPHandler) CreateQuiz(w http.ResponseWriter, r *http.Request) {
	resourceIDStr := chi.URLParam(r, "resourceId")
	resourceID, err := uuid.Parse(resourceIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de recurso inválido")
		return
	}

	var quiz domain.Quiz
	if err := json.NewDecoder(r.Body).Decode(&quiz); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}
	quiz.ResourceID = resourceID

	createdQuiz, err := h.useCase.CreateQuiz(r.Context(), uuid.Nil, &quiz)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, createdQuiz)
}

func (h *HTTPHandler) GetQuizSnapshot(w http.ResponseWriter, r *http.Request) {
	quizIDStr := chi.URLParam(r, "quizId")
	quizID, err := uuid.Parse(quizIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de quiz inválido")
		return
	}

	snapshot, err := h.useCase.GetQuizSnapshot(r.Context(), quizID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Quiz no encontrado")
		return
	}

	respondJSON(w, http.StatusOK, snapshot)
}

func (h *HTTPHandler) StartQuizAttempt(w http.ResponseWriter, r *http.Request) {
	quizIDStr := chi.URLParam(r, "quizId")
	quizID, err := uuid.Parse(quizIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de quiz inválido")
		return
	}

	authUser := middleware.GetUserFromContext(r.Context())
	studentID := uuid.Nil
	if authUser != nil {
		studentID = authUser.ID
	}

	var req struct {
		StudentID string `json:"student_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if studentID == uuid.Nil && req.StudentID != "" {
		studentID, _ = uuid.Parse(req.StudentID)
	}

	if studentID == uuid.Nil {
		respondError(w, http.StatusUnauthorized, "Se requiere estudiante autenticado")
		return
	}

	attempt, err := h.useCase.StartQuizAttempt(r.Context(), studentID, quizID)
	if err != nil {
		if err == domain.ErrMaxAttemptsReached {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, attempt)
}

func (h *HTTPHandler) SavePartialAttempt(w http.ResponseWriter, r *http.Request) {
	attemptIDStr := chi.URLParam(r, "attemptId")
	attemptID, err := uuid.Parse(attemptIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de intento inválido")
		return
	}

	authUser := middleware.GetUserFromContext(r.Context())
	studentID := uuid.Nil
	if authUser != nil {
		studentID = authUser.ID
	}

	var req struct {
		StudentID string            `json:"student_id"`
		Answers   map[string]string `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	if studentID == uuid.Nil && req.StudentID != "" {
		studentID, _ = uuid.Parse(req.StudentID)
	}

	attempt, err := h.useCase.SavePartialAttempt(r.Context(), studentID, attemptID, req.Answers)
	if err != nil {
		if err == domain.ErrAttemptAlreadySubmitted {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, attempt)
}

func (h *HTTPHandler) SubmitQuizAttempt(w http.ResponseWriter, r *http.Request) {
	quizIDStr := chi.URLParam(r, "quizId")
	attemptIDStr := chi.URLParam(r, "attemptId")

	var quizID uuid.UUID
	var attemptID *uuid.UUID

	if quizIDStr != "" {
		quizID, _ = uuid.Parse(quizIDStr)
	}
	if attemptIDStr != "" {
		if aid, err := uuid.Parse(attemptIDStr); err == nil {
			attemptID = &aid
		}
	}

	authUser := middleware.GetUserFromContext(r.Context())
	studentID := uuid.Nil
	if authUser != nil {
		studentID = authUser.ID
	}

	var req struct {
		AttemptID string            `json:"attempt_id"`
		StudentID string            `json:"student_id"`
		Answers   map[string]string `json:"answers"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if attemptID == nil && req.AttemptID != "" {
		if aid, err := uuid.Parse(req.AttemptID); err == nil {
			attemptID = &aid
		}
	}

	if studentID == uuid.Nil && req.StudentID != "" {
		studentID, _ = uuid.Parse(req.StudentID)
	}

	if studentID == uuid.Nil {
		respondError(w, http.StatusUnauthorized, "Se requiere estudiante autenticado")
		return
	}

	attempt, err := h.useCase.SubmitQuizAttempt(r.Context(), studentID, quizID, attemptID, req.Answers)
	if err != nil {
		if err == domain.ErrMaxAttemptsReached || err == domain.ErrAttemptAlreadySubmitted {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		if err == domain.ErrForbidden {
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, attempt)
}

func (h *HTTPHandler) RecordHeartbeat(w http.ResponseWriter, r *http.Request) {
	authUser := middleware.GetUserFromContext(r.Context())

	var input HeartbeatInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	if authUser != nil && input.StudentID == uuid.Nil {
		input.StudentID = authUser.ID
	}

	enrollment, err := h.useCase.RecordHeartbeat(r.Context(), input)
	if err != nil {
		if err == domain.ErrNotEnrolled {
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, enrollment)
}

func (h *HTTPHandler) VerifyBadge(w http.ResponseWriter, r *http.Request) {
	codeStr := chi.URLParam(r, "code")
	code, err := uuid.Parse(codeStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "Código de verificación de insignia inválido")
		return
	}

	badge, err := h.useCase.VerifyBadge(r.Context(), code)
	if err != nil {
		respondError(w, http.StatusNotFound, "Insignia no encontrada o inválida")
		return
	}

	respondJSON(w, http.StatusOK, badge)
}

func (h *HTTPHandler) RevokeBadge(w http.ResponseWriter, r *http.Request) {
	badgeIDStr := chi.URLParam(r, "badgeId")
	badgeID, err := uuid.Parse(badgeIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de insignia inválido")
		return
	}

	authUser := middleware.GetUserFromContext(r.Context())
	adminID := uuid.Nil
	if authUser != nil {
		adminID = authUser.ID
	}
	if adminID == uuid.Nil {
		adminID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	}

	if err := h.useCase.RevokeBadge(r.Context(), adminID, badgeID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Insignia revocada exitosamente"})
}
