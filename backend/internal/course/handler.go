package course

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

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
	r.Route("/api/v1/courses", func(r chi.Router) {
		r.Get("/", h.ListCourses)
		r.Post("/", h.CreateCourse)
		r.Get("/{id}", h.GetCourseHierarchy)
		r.Get("/{id}/preview", h.GetCourseDraftPreview)
		r.Post("/{id}/unpublish", h.UnpublishCourse)
		r.Post("/{id}/create-draft", h.CreateDraftFromPublished)

		r.Post("/versions/{versionId}/modules", h.AddModule)
		r.Patch("/versions/{versionId}/reorder-modules", h.ReorderModules)

		r.Post("/modules/{moduleId}/units", h.AddUnit)
		r.Patch("/modules/{moduleId}/reorder-units", h.ReorderUnits)

		r.Post("/units/{unitId}/resources", h.AddResource)
		r.Patch("/units/{unitId}/reorder-resources", h.ReorderResources)

		r.Put("/resources/{id}", h.UpdateResource)
		r.Patch("/resources/{id}/autosave", h.AutosaveResource)
		r.Delete("/resources/{id}", h.DeleteResource)

		r.Post("/versions/{versionId}/publish", h.PublishVersion)
	})
}

type APIResponse struct {
	Success bool     `json:"success"`
	Data    any      `json:"data,omitempty"`
	Error   string   `json:"error,omitempty"`
	// Details lista TODOS los problemas de validacion de publicacion
	// (ver domain.PublishValidationError), no solo uno.
	Details []string `json:"details,omitempty"`
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

func respondValidationError(w http.ResponseWriter, status int, message string, details []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(APIResponse{Success: false, Error: message, Details: details})
}

func (h *HTTPHandler) ListCourses(w http.ResponseWriter, r *http.Request) {
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit, _ := strconv.Atoi(limitStr)
	if limit <= 0 {
		limit = 20
	}
	offset, _ := strconv.Atoi(offsetStr)

	courses, err := h.useCase.GetCourseCatalog(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, courses)
}

func (h *HTTPHandler) CreateCourse(w http.ResponseWriter, r *http.Request) {
	authUser := middleware.GetUserFromContext(r.Context())
	teacherID := uuid.Nil
	if authUser != nil {
		teacherID = authUser.ID
	} else {
		teacherIDStr := r.Header.Get("X-Teacher-ID")
		if teacherIDStr == "" {
			teacherIDStr = r.Header.Get("X-Admin-ID")
		}
		if teacherIDStr != "" {
			teacherID, _ = uuid.Parse(teacherIDStr)
		}
	}

	if teacherID == uuid.Nil {
		teacherID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	}

	var input CreateCourseInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	course, version, err := h.useCase.CreateCourse(r.Context(), teacherID, input)
	if err != nil {
		if err == domain.ErrUnauthorizedCourseMutation {
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"course":  course,
		"version": version,
	})
}

func (h *HTTPHandler) GetCourseHierarchy(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	courseID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de curso inválido")
		return
	}

	hierarchy, err := h.useCase.GetCourseHierarchy(r.Context(), courseID)
	if err != nil {
		if err == domain.ErrCourseNotFound || err == domain.ErrVersionNotFound {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, hierarchy)
}

func (h *HTTPHandler) GetCourseDraftPreview(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	courseID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de curso inválido")
		return
	}

	draft, err := h.useCase.GetCourseDraftPreview(r.Context(), uuid.Nil, courseID)
	if err != nil {
		if err == domain.ErrVersionNotFound {
			respondError(w, http.StatusNotFound, "No existe un borrador de actualización disponible")
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, draft)
}

func (h *HTTPHandler) CreateDraftFromPublished(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	courseID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de curso inválido")
		return
	}

	draft, err := h.useCase.CreateDraftFromPublished(r.Context(), uuid.Nil, courseID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, draft)
}

func (h *HTTPHandler) UnpublishCourse(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	courseID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de curso inválido")
		return
	}

	if err := h.useCase.UnpublishCourse(r.Context(), uuid.Nil, courseID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Curso despublicado temporalmente"})
}

func (h *HTTPHandler) AddModule(w http.ResponseWriter, r *http.Request) {
	versionIDStr := chi.URLParam(r, "versionId")
	versionID, err := uuid.Parse(versionIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de versión inválido")
		return
	}

	var req struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Position    int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	mod, err := h.useCase.AddModule(r.Context(), uuid.Nil, versionID, req.Title, req.Description, req.Position)
	if err != nil {
		if err == domain.ErrVersionImmutable {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, mod)
}

func (h *HTTPHandler) AddUnit(w http.ResponseWriter, r *http.Request) {
	moduleIDStr := chi.URLParam(r, "moduleId")
	moduleID, err := uuid.Parse(moduleIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de módulo inválido")
		return
	}

	var req struct {
		Title    string `json:"title"`
		Position int    `json:"position"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	unit, err := h.useCase.AddUnit(r.Context(), uuid.Nil, moduleID, req.Title, req.Position)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, unit)
}

func (h *HTTPHandler) AddResource(w http.ResponseWriter, r *http.Request) {
	unitIDStr := chi.URLParam(r, "unitId")
	unitID, err := uuid.Parse(unitIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de unidad inválido")
		return
	}

	var res domain.Resource
	if err := json.NewDecoder(r.Body).Decode(&res); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}
	res.UnitID = unitID

	createdRes, err := h.useCase.AddResource(r.Context(), uuid.Nil, &res)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusCreated, createdRes)
}

func (h *HTTPHandler) UpdateResource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	resourceID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de recurso inválido")
		return
	}

	var fields UpdateResourceFields
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	updatedRes, err := h.useCase.UpdateResource(r.Context(), uuid.Nil, resourceID, fields)
	if err != nil {
		if err == domain.ErrResourceNotFound {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, updatedRes)
}

// AutosaveResource guarda SOLO title/canonical_markdown con un timestamp
// de autosave distinto del guardado explicito (PUT). Pensado para que un
// editor lo llame cada pocos segundos mientras el usuario escribe, sin
// reenviar el recurso completo y sin riesgo de tocar visibilidad/orden.
func (h *HTTPHandler) AutosaveResource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	resourceID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de recurso inválido")
		return
	}

	var payload struct {
		Title             *string `json:"title"`
		CanonicalMarkdown *string `json:"canonical_markdown"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}
	if payload.Title == nil && payload.CanonicalMarkdown == nil {
		respondError(w, http.StatusBadRequest, "autosave requiere al menos 'title' o 'canonical_markdown'")
		return
	}

	updatedRes, err := h.useCase.AutosaveResource(r.Context(), uuid.Nil, resourceID, payload.Title, payload.CanonicalMarkdown)
	if err != nil {
		if err == domain.ErrResourceNotFound {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, updatedRes)
}

func (h *HTTPHandler) DeleteResource(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	resourceID, err := uuid.Parse(idStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de recurso inválido")
		return
	}

	if err := h.useCase.DeleteResource(r.Context(), uuid.Nil, resourceID); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Recurso eliminado exitosamente"})
}

func (h *HTTPHandler) ReorderModules(w http.ResponseWriter, r *http.Request) {
	versionIDStr := chi.URLParam(r, "versionId")
	versionID, err := uuid.Parse(versionIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de versión inválido")
		return
	}

	var req struct {
		OrderedIDs []uuid.UUID `json:"ordered_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	if err := h.useCase.ReorderModules(r.Context(), uuid.Nil, versionID, req.OrderedIDs); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Módulos reordenados exitosamente"})
}

func (h *HTTPHandler) ReorderUnits(w http.ResponseWriter, r *http.Request) {
	moduleIDStr := chi.URLParam(r, "moduleId")
	moduleID, err := uuid.Parse(moduleIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de módulo inválido")
		return
	}

	var req struct {
		OrderedIDs []uuid.UUID `json:"ordered_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	if err := h.useCase.ReorderUnits(r.Context(), uuid.Nil, moduleID, req.OrderedIDs); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Unidades reordenadas exitosamente"})
}

func (h *HTTPHandler) ReorderResources(w http.ResponseWriter, r *http.Request) {
	unitIDStr := chi.URLParam(r, "unitId")
	unitID, err := uuid.Parse(unitIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de unidad inválido")
		return
	}

	var req struct {
		OrderedIDs []uuid.UUID `json:"ordered_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Payload JSON inválido")
		return
	}

	if err := h.useCase.ReorderResources(r.Context(), uuid.Nil, unitID, req.OrderedIDs); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"message": "Recursos reordenados exitosamente"})
}

func (h *HTTPHandler) PublishVersion(w http.ResponseWriter, r *http.Request) {
	versionIDStr := chi.URLParam(r, "versionId")
	versionID, err := uuid.Parse(versionIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de versión inválido")
		return
	}

	pubVersion, err := h.useCase.PublishVersion(r.Context(), uuid.Nil, versionID)
	if err != nil {
		var validationErr *domain.PublishValidationError
		if errors.As(err, &validationErr) {
			respondValidationError(w, http.StatusBadRequest, "la versión no cumple los requisitos de publicación", validationErr.Issues)
			return
		}
		if err == domain.ErrVersionImmutable {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, pubVersion)
}
