package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/mooc-platform/backend/internal/domain"
	"github.com/mooc-platform/backend/internal/middleware"
	"github.com/mooc-platform/backend/internal/worker"
)

// asynqDefaultQueue es la cola en la que se encolan las tareas de este
// paquete (worker.NewMediaTranscodeHLSTask no pasa asynq.Queue(...), así
// que caen en la cola por defecto de asynq).
const asynqDefaultQueue = "default"

type HTTPHandler struct {
	storage      *StorageService
	courseRepo   domain.CourseRepository
	learningRepo domain.LearningRepository
	asynqClient  *asynq.Client
	// asynqInspector es opcional: si es nil, seguimos pudiendo encolar,
	// simplemente perdemos la capacidad de destrabar una tarea que quedó
	// archivada con el mismo Task ID (ver enqueueTranscodeTask).
	asynqInspector *asynq.Inspector
}

func NewHTTPHandler(storage *StorageService, courseRepo domain.CourseRepository, learningRepo domain.LearningRepository, asynqClient *asynq.Client, asynqInspector *asynq.Inspector) *HTTPHandler {
	return &HTTPHandler{
		storage:        storage,
		courseRepo:     courseRepo,
		learningRepo:   learningRepo,
		asynqClient:    asynqClient,
		asynqInspector: asynqInspector,
	}
}

func (h *HTTPHandler) RegisterRoutes(r chi.Router) {
	r.Route("/api/v1/media", func(r chi.Router) {
		r.Post("/presigned-upload", h.GeneratePresignedUpload)
		r.Get("/presigned-download", h.GeneratePresignedDownload)
		r.Post("/resources/{resourceId}/complete-upload", h.CompleteUpload)
		r.Get("/resources/{resourceId}/stream-url", h.GetStreamURL)
		r.Get("/resources/{resourceId}/manifest.m3u8", h.GetSignedManifest)
		r.Get("/resources/{resourceId}/resume", h.GetResumePosition)
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

// publicBaseURL reconstruye el esquema+host públicos con los que se llamó a
// la API, respetando cabeceras de un proxy/balanceador si están presentes.
// Se usa para devolver URLs absolutas hacia endpoints propios (como el
// manifiesto HLS firmado), en vez de asumir un host fijo.
func publicBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}

	host := r.Host
	if fwdHost := r.Header.Get("X-Forwarded-Host"); fwdHost != "" {
		host = fwdHost
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}

func (h *HTTPHandler) GeneratePresignedUpload(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ObjectKey   string `json:"object_key"`
		ContentType string `json:"content_type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ObjectKey == "" {
		respondError(w, http.StatusBadRequest, "Se requiere object_key válido")
		return
	}

	out, err := h.storage.GeneratePresignedUploadURL(r.Context(), req.ObjectKey, req.ContentType)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, out)
}

func (h *HTTPHandler) GeneratePresignedDownload(w http.ResponseWriter, r *http.Request) {
	objectKey := r.URL.Query().Get("object_key")
	if objectKey == "" {
		respondError(w, http.StatusBadRequest, "Se requiere el parámetro object_key")
		return
	}

	downloadURL, err := h.storage.GeneratePresignedDownloadURL(r.Context(), objectKey, 1*time.Hour)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"download_url": downloadURL,
	})
}

func (h *HTTPHandler) CompleteUpload(w http.ResponseWriter, r *http.Request) {
	resourceIDStr := chi.URLParam(r, "resourceId")
	resourceID, err := uuid.Parse(resourceIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de recurso inválido")
		return
	}

	var req struct {
		ObjectKey string `json:"object_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ObjectKey == "" {
		respondError(w, http.StatusBadRequest, "Se requiere object_key válido")
		return
	}

	// 1. Validar que el objeto existe en S3
	stat, err := h.storage.StatObject(r.Context(), req.ObjectKey)
	if err != nil {
		respondError(w, http.StatusBadRequest, "El archivo no se encuentra disponible en almacenamiento S3")
		return
	}

	// 2. Obtener recurso y actualizar estado a processing
	res, err := h.courseRepo.GetResourceByID(r.Context(), resourceID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Recurso no encontrado")
		return
	}

	res.MediaURL = req.ObjectKey
	res.ProcessingStatus = domain.ProcessingInProgress
	if err := h.courseRepo.UpdateResource(r.Context(), res); err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 3. Encolar tarea asíncrona en Asynq
	if h.asynqClient != nil && (res.Type == domain.ResourceTypeVideo || res.Type == domain.ResourceTypeAudio) {
		task, err := worker.NewMediaTranscodeHLSTask(res.ID, req.ObjectKey, string(res.Type))
		if err != nil {
			log.Printf("[MEDIA] No se pudo construir la tarea de transcodificación para %s: %v", res.ID, err)
			respondError(w, http.StatusInternalServerError, "No se pudo preparar el procesamiento del recurso")
			return
		}

		if err := h.enqueueTranscodeTask(r.Context(), task, res.ID.String()); err != nil {
			// Antes este error se descartaba en silencio (_, _ = ...Enqueue(task)):
			// el recurso quedaba en "processing" para siempre y nadie se enteraba
			// de que la transcodificación nunca se había disparado. Ahora lo
			// registramos, lo reflejamos en el estado del recurso y se lo
			// devolvemos al llamador para que pueda reintentar.
			log.Printf("[MEDIA] Fallo encolando transcodificación HLS para %s: %v", res.ID, err)

			res.ProcessingStatus = domain.ProcessingFailed
			if updErr := h.courseRepo.UpdateResource(r.Context(), res); updErr != nil {
				log.Printf("[MEDIA] Además falló marcando %s como failed: %v", res.ID, updErr)
			}

			respondError(w, http.StatusBadGateway, "El archivo se subió, pero no se pudo encolar el procesamiento. Reintentá el complete-upload.")
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message":           "Carga validada exitosamente; procesamiento encolado",
		"resource_id":       res.ID,
		"object_size_bytes": stat.Size,
		"processing_status": res.ProcessingStatus,
	})
}

// enqueueTranscodeTask encola la tarea de transcodificación HLS. El worker
// usa el resource ID como Task ID (asynq.TaskID) para lograr idempotencia
// ante encolados duplicados, pero eso tiene un efecto secundario: asynq
// rechaza (asynq.ErrTaskIDConflict) cualquier intento de reencolar una
// tarea con ese mismo ID, incluso si esa tarea ya terminó en la
// Dead-Letter Queue. Sin este manejo, un complete-upload repetido tras un
// fallo transitorio (p.ej. la BD del worker caída) nunca vuelve a disparar
// el procesamiento, y el recurso queda atascado para siempre.
func (h *HTTPHandler) enqueueTranscodeTask(ctx context.Context, task *asynq.Task, taskID string) error {
	if _, err := h.asynqClient.Enqueue(task); err == nil {
		return nil
	} else if !errors.Is(err, asynq.ErrTaskIDConflict) {
		return fmt.Errorf("enqueue failed: %w", err)
	}

	if h.asynqInspector == nil {
		// No podemos saber en qué estado quedó la tarea existente; lo más
		// seguro es asumir que sigue viva y no tratar esto como error fatal.
		return nil
	}

	info, err := h.asynqInspector.GetTaskInfo(asynqDefaultQueue, taskID)
	if err != nil {
		log.Printf("[MEDIA] No se pudo inspeccionar la tarea %s tras conflicto de Task ID: %v", taskID, err)
		return nil
	}

	switch info.State {
	case asynq.TaskStateArchived, asynq.TaskStateCompleted:
		// Tarea terminal (falló y fue enviada a la DLQ, o ya se completó
		// hace tiempo): la limpiamos y reintentamos el encolado para que
		// este complete-upload realmente dispare un intento nuevo.
		if err := h.asynqInspector.DeleteTask(asynqDefaultQueue, taskID); err != nil {
			return fmt.Errorf("no se pudo limpiar la tarea archivada %s: %w", taskID, err)
		}
		if _, err := h.asynqClient.Enqueue(task); err != nil {
			return fmt.Errorf("reintento de enqueue falló tras limpiar tarea archivada: %w", err)
		}
		return nil
	default:
		// Pending, Active, Scheduled o Retry: ya hay una tarea viva para
		// este recurso, no hace falta (ni conviene) duplicarla.
		return nil
	}
}

func (h *HTTPHandler) GetStreamURL(w http.ResponseWriter, r *http.Request) {
	resourceIDStr := chi.URLParam(r, "resourceId")
	resourceID, err := uuid.Parse(resourceIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de recurso inválido")
		return
	}

	res, err := h.courseRepo.GetResourceByID(r.Context(), resourceID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Recurso no encontrado")
		return
	}

	// authUser no se usaba en ningún punto posterior de esta función (el
	// valor asignado en el fallback era una "ineffectual assignment" real,
	// detectada por ineffassign) — se elimina en vez de mantener código
	// muerto. Si en el futuro GetStreamURL necesita autorizar por usuario,
	// reintroducir middleware.GetUserFromContext aquí y usarlo de verdad.
	var streamURL string
	if IsHLSManifestKey(res.MediaURL) {
		// res.MediaURL es un manifiesto HLS (hls/<id>/master.m3u8). Un
		// presigned URL solo firma ESE objeto: los segmentos .ts que el
		// manifiesto referencia con rutas relativas son objetos aparte en un
		// bucket privado, así que el reproductor no podría bajarlos. En vez
		// de firmar el manifiesto directamente, devolvemos la URL de nuestro
		// propio endpoint, que lo reescribe firmando cada segmento al vuelo.
		streamURL = fmt.Sprintf("%s/api/v1/media/resources/%s/manifest.m3u8", publicBaseURL(r), res.ID)
	} else {
		streamURL, err = h.storage.GeneratePresignedDownloadURL(r.Context(), res.MediaURL, 2*time.Hour)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"resource_id":   res.ID,
		"title":         res.Title,
		"type":          res.Type,
		"presigned_url": streamURL,
	})
}

// variantNamePattern restringe ?variant= a un nombre plano de playlist HLS
// (p. ej. "720p.m3u8"), sin separadores de ruta.
var variantNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+\.m3u8$`)

// GetSignedManifest sirve el manifiesto HLS de un recurso ya transcodificado,
// con cada línea de segmento/sub-playlist reescrita como una URL prefirmada
// de S3/MinIO. Ver el comentario en GetStreamURL para el porqué.
func (h *HTTPHandler) GetSignedManifest(w http.ResponseWriter, r *http.Request) {
	resourceIDStr := chi.URLParam(r, "resourceId")
	resourceID, err := uuid.Parse(resourceIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de recurso inválido")
		return
	}

	res, err := h.courseRepo.GetResourceByID(r.Context(), resourceID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Recurso no encontrado")
		return
	}

	if res.ProcessingStatus != domain.ProcessingCompleted || !IsHLSManifestKey(res.MediaURL) {
		respondError(w, http.StatusConflict, "El recurso no tiene un manifiesto HLS disponible")
		return
	}

	manifestKey := res.MediaURL
	var variantURL func(entry string) string
	if variant := r.URL.Query().Get("variant"); variant != "" {
		// Sub-playlist de una calidad (p. ej. 720p.m3u8) del manifiesto
		// maestro. El nombre sale de la query, así que solo se aceptan
		// nombres planos: nada de rutas ni "..".
		if !variantNamePattern.MatchString(variant) {
			respondError(w, http.StatusBadRequest, "Variante HLS inválida")
			return
		}
		manifestKey = path.Join(path.Dir(res.MediaURL), variant)
		if _, statErr := h.storage.StatObject(r.Context(), manifestKey); statErr != nil {
			respondError(w, http.StatusNotFound, "Variante HLS no encontrada")
			return
		}
	} else {
		// Manifiesto maestro: sus sub-playlists vuelven a este endpoint
		// (?variant=) para que sus segmentos también salgan firmados.
		base := publicBaseURL(r)
		variantURL = func(entry string) string {
			return fmt.Sprintf("%s/api/v1/media/resources/%s/manifest.m3u8?variant=%s", base, res.ID, url.QueryEscape(entry))
		}
	}

	body, err := h.storage.RewriteHLSManifest(r.Context(), manifestKey, variantURL)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "No se pudo generar el manifiesto firmado")
		return
	}

	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	// El contenido trae URLs firmadas con expiración propia; no debe cachearse.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (h *HTTPHandler) GetResumePosition(w http.ResponseWriter, r *http.Request) {
	resourceIDStr := chi.URLParam(r, "resourceId")
	resourceID, err := uuid.Parse(resourceIDStr)
	if err != nil {
		respondError(w, http.StatusBadRequest, "ID de recurso inválido")
		return
	}

	res, err := h.courseRepo.GetResourceByID(r.Context(), resourceID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Recurso no encontrado")
		return
	}

	authUser := middleware.GetUserFromContext(r.Context())
	var lastPosition int
	if authUser != nil && h.learningRepo != nil {
		progressList, err := h.learningRepo.GetStudentCourseProgress(r.Context(), authUser.ID, uuid.Nil)
		if err == nil {
			for _, p := range progressList {
				if p.ResourceStableID == res.StableID {
					lastPosition = p.LastPositionSeconds
					break
				}
			}
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"resource_id":           res.ID,
		"stable_id":             res.StableID,
		"last_position_seconds": lastPosition,
	})
}
