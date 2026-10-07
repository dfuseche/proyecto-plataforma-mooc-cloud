package user

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

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
	r.Route("/api/v1/auth", func(r chi.Router) {
		r.Post("/register", h.RegisterStudent)
		r.Post("/verify-email", h.VerifyEmail)
		r.Post("/login", h.Login)
		r.Post("/logout", h.Logout)
		r.Post("/forgot-password", h.ForgotPassword)
		r.Post("/reset-password", h.ResetPassword)
	})

	r.Route("/api/v1/admin", func(r chi.Router) {
		r.Get("/users", h.ListUsers)
		r.Post("/teachers", h.CreateTeacher)
		r.Post("/users/invite-teacher", h.CreateTeacher)
		r.Patch("/users/{id}/status", h.ChangeUserStatus)
		r.Get("/audit-logs", h.ListAuditLogs)
	})
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

func (h *HTTPHandler) RegisterStudent(w http.ResponseWriter, r *http.Request) {
	var input RegisterStudentInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	user, token, err := h.useCase.RegisterStudent(r.Context(), input)
	if err != nil {
		if err == domain.ErrUserAlreadyExists {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"user":              user,
		"verification_code": token,
	})
}

func (h *HTTPHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		respondError(w, http.StatusBadRequest, "Se requiere el parámetro token")
		return
	}

	if err := h.useCase.VerifyEmail(r.Context(), req.Token); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Correo electrónico verificado exitosamente"})
}

func (h *HTTPHandler) Login(w http.ResponseWriter, r *http.Request) {
	var input LoginInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	input.UserAgent = r.UserAgent()
	input.IPAddress = r.RemoteAddr

	output, err := h.useCase.Login(r.Context(), input)
	if err != nil {
		if err == domain.ErrInvalidCredentials || err == domain.ErrEmailNotVerified {
			respondError(w, http.StatusUnauthorized, err.Error())
			return
		}
		if err == domain.ErrUserSuspended {
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, output)
}

func (h *HTTPHandler) Logout(w http.ResponseWriter, r *http.Request) {
	tokenStr := ""
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		tokenStr = strings.TrimPrefix(authHeader, "Bearer ")
	}

	if session := middleware.GetSessionFromContext(r.Context()); session != nil {
		tokenStr = session.Token
	}

	if tokenStr != "" {
		_ = h.useCase.Logout(r.Context(), tokenStr)
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Sesión cerrada exitosamente"})
}

func (h *HTTPHandler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
		respondError(w, http.StatusBadRequest, "Se requiere email válido")
		return
	}

	resetToken, err := h.useCase.ForgotPassword(r.Context(), req.Email)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message":     "Instrucciones enviadas si el correo está registrado",
		"reset_token": resetToken, // En producción se envía por correo
	})
}

func (h *HTTPHandler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token       string `json:"token"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" || req.NewPassword == "" {
		respondError(w, http.StatusBadRequest, "Token y new_password son requeridos")
		return
	}

	if err := h.useCase.ResetPassword(r.Context(), req.Token, req.NewPassword); err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Contraseña restablecida exitosamente"})
}

func (h *HTTPHandler) CreateTeacher(w http.ResponseWriter, r *http.Request) {
	authUser := middleware.GetUserFromContext(r.Context())
	adminID := uuid.Nil
	if authUser != nil {
		adminID = authUser.ID
	} else {
		adminIDStr := r.Header.Get("X-Admin-ID")
		if adminIDStr != "" {
			adminID, _ = uuid.Parse(adminIDStr)
		}
	}

	if adminID == uuid.Nil {
		adminID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	}

	var input CreateTeacherInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	teacher, err := h.useCase.CreateTeacher(r.Context(), adminID, input)
	if err != nil {
		if err == domain.ErrForbidden {
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		if err == domain.ErrUserAlreadyExists {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, teacher)
}

func (h *HTTPHandler) ChangeUserStatus(w http.ResponseWriter, r *http.Request) {
	authUser := middleware.GetUserFromContext(r.Context())
	adminID := uuid.Nil
	if authUser != nil {
		adminID = authUser.ID
	} else {
		adminIDStr := r.Header.Get("X-Admin-ID")
		if adminIDStr != "" {
			adminID, _ = uuid.Parse(adminIDStr)
		}
	}

	if adminID == uuid.Nil {
		adminID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	}

	targetIDStr := chi.URLParam(r, "id")
	targetID, err := uuid.Parse(targetIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de usuario inválido")
		return
	}

	var req struct {
		Status domain.UserStatus `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	if err := h.useCase.ChangeUserStatus(r.Context(), adminID, targetID, req.Status); err != nil {
		if err == domain.ErrLastAdminProtection {
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

	respondJSON(w, http.StatusOK, map[string]string{"message": "Estado de usuario actualizado exitosamente"})
}

func (h *HTTPHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	roleStr := q.Get("role")
	statusStr := q.Get("status")
	search := q.Get("search")
	limitStr := q.Get("limit")
	offsetStr := q.Get("offset")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	var role *domain.Role
	if roleStr != "" {
		r := domain.Role(roleStr)
		role = &r
	}

	var status *domain.UserStatus
	if statusStr != "" {
		s := domain.UserStatus(statusStr)
		status = &s
	}

	users, total, err := h.useCase.ListUsers(r.Context(), role, status, search, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"users":  users,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (h *HTTPHandler) ListAuditLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limitStr := q.Get("limit")
	offsetStr := q.Get("offset")

	limit, _ := strconv.Atoi(limitStr)
	offset, _ := strconv.Atoi(offsetStr)

	logs, total, err := h.useCase.ListAuditLogs(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"audit_logs": logs,
		"total":      total,
		"limit":      limit,
		"offset":     offset,
	})
}
