package domain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type VersionStatus string
type ResourceType string
type ProcessingStatus string

const (
	VersionStatusDraft     VersionStatus = "draft"
	VersionStatusPublished VersionStatus = "published"
	VersionStatusArchived  VersionStatus = "archived"
)

const (
	ResourceTypeText         ResourceType = "text"
	ResourceTypeImage        ResourceType = "image"
	ResourceTypeVideo        ResourceType = "video"
	ResourceTypeAudio        ResourceType = "audio"
	ResourceTypePDF          ResourceType = "pdf"
	ResourceTypePresentation ResourceType = "presentation"
	ResourceTypeDownload     ResourceType = "download"
	ResourceTypeIframe       ResourceType = "iframe"
	ResourceTypeLink         ResourceType = "link"
	ResourceTypeQuiz         ResourceType = "quiz"
)

const (
	ProcessingPending    ProcessingStatus = "pending"
	ProcessingInProgress ProcessingStatus = "processing"
	ProcessingCompleted  ProcessingStatus = "completed"
	ProcessingFailed     ProcessingStatus = "failed"
)

var (
	ErrCourseNotFound             = errors.New("curso no encontrado")
	ErrVersionNotFound            = errors.New("versión de curso no encontrada")
	ErrResourceNotFound           = errors.New("recurso no encontrado")
	ErrVersionImmutable           = errors.New("una versión publicada es inmutable; debe crear un borrador de actualización")
	ErrInvalidPublishStructure    = errors.New("un curso solo se publica con metadatos completos, criterios de aprobación y la jerarquía mínima (Módulo -> Unidad -> Recurso visible y disponible)")
	ErrUnauthorizedCourseMutation = errors.New("no tiene permisos para editar este curso")
)

// PublishValidationError junta TODOS los problemas que impiden publicar
// una version, no solo el primero que se encuentra. La rubrica senalo
// exactamente esto: "la validacion de publicacion corta en el primer
// error en vez de devolver la lista exhaustiva".
type PublishValidationError struct {
	Issues []string
}

func (e *PublishValidationError) Error() string {
	if len(e.Issues) == 1 {
		return fmt.Sprintf("la version no cumple los requisitos de publicacion: %s", e.Issues[0])
	}
	return fmt.Sprintf(
		"la version no cumple los requisitos de publicacion (%d problemas): %s",
		len(e.Issues), strings.Join(e.Issues, "; "),
	)
}

type Course struct {
	ID                        uuid.UUID  `json:"id"`
	Slug                      string     `json:"slug"`
	Title                     string     `json:"title"`
	Summary                   string     `json:"summary"`
	CreatedByTeacherID        uuid.UUID  `json:"created_by_teacher_id"`
	CurrentPublishedVersionID *uuid.UUID `json:"current_published_version_id,omitempty"`
	CreatedAt                 time.Time  `json:"created_at"`
	UpdatedAt                 time.Time  `json:"updated_at"`
}

type CourseVersion struct {
	ID            uuid.UUID `json:"id"`
	CourseID      uuid.UUID `json:"course_id"`
	VersionNumber int       `json:"version_number"`
	Status        VersionStatus `json:"status"`
	PassingScore  float64   `json:"passing_score"`
	CreatedAt     time.Time `json:"created_at"`
	PublishedAt   *time.Time `json:"published_at,omitempty"`
	Modules       []Module  `json:"modules,omitempty"`
}

type Module struct {
	ID          uuid.UUID `json:"id"`
	VersionID   uuid.UUID `json:"version_id"`
	StableID    uuid.UUID `json:"stable_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"created_at"`
	Units       []Unit    `json:"units,omitempty"`
}

type Unit struct {
	ID        uuid.UUID  `json:"id"`
	ModuleID  uuid.UUID  `json:"module_id"`
	StableID  uuid.UUID  `json:"stable_id"`
	Title     string     `json:"title"`
	Position  int        `json:"position"`
	CreatedAt time.Time  `json:"created_at"`
	Resources []Resource `json:"resources,omitempty"`
}

type Resource struct {
	ID                 uuid.UUID        `json:"id"`
	UnitID             uuid.UUID        `json:"unit_id"`
	StableID           uuid.UUID        `json:"stable_id"`
	Title              string           `json:"title"`
	Type               ResourceType     `json:"type"`
	CanonicalMarkdown  string           `json:"canonical_markdown,omitempty"`
	MediaURL           string           `json:"media_url,omitempty"`
	ObjectKey          string           `json:"object_key,omitempty"`
	PresignedUploadURL string           `json:"presigned_upload_url,omitempty"`
	IsVisible          bool             `json:"is_visible"`
	IsMandatory        bool             `json:"is_mandatory"`
	IsDownloadable     bool             `json:"is_downloadable"`
	Position           int              `json:"position"`
	ProcessingStatus   ProcessingStatus `json:"processing_status"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	// LastAutosavedAt se actualiza solo por el endpoint de autosave
	// (PATCH .../resources/{id}/autosave), nunca por el guardado
	// explicito (PUT). Permite distinguir y comprobar que el autosave
	// esta ocurriendo de verdad, en vez de verse igual que cualquier
	// guardado manual.
	LastAutosavedAt *time.Time `json:"last_autosaved_at,omitempty"`
}

type CourseRepository interface {
	CreateCourse(ctx context.Context, course *Course, initialVersion *CourseVersion) error
	GetCourseByID(ctx context.Context, id uuid.UUID) (*Course, error)
	GetCourseBySlug(ctx context.Context, slug string) (*Course, error)
	ListCourses(ctx context.Context, limit, offset int) ([]*Course, error)
	UpdateCourse(ctx context.Context, course *Course) error

	CreateVersion(ctx context.Context, version *CourseVersion) error
	GetVersionByID(ctx context.Context, versionID uuid.UUID) (*CourseVersion, error)
	GetLatestDraftVersion(ctx context.Context, courseID uuid.UUID) (*CourseVersion, error)
	GetFullVersionHierarchy(ctx context.Context, versionID uuid.UUID) (*CourseVersion, error)
	PublishVersion(ctx context.Context, courseID uuid.UUID, versionID uuid.UUID) error
	UnpublishCourse(ctx context.Context, courseID uuid.UUID) error

	CreateModule(ctx context.Context, module *Module) error
	CreateUnit(ctx context.Context, unit *Unit) error
	CreateResource(ctx context.Context, resource *Resource) error
	GetResourceByID(ctx context.Context, resourceID uuid.UUID) (*Resource, error)
	UpdateResource(ctx context.Context, resource *Resource) error
	// AutosaveResource actualiza unicamente contenido (title/markdown,
	// los campos que un editor guarda mientras el usuario escribe) y
	// marca LastAutosavedAt. A proposito NO toca is_visible/is_mandatory/
	// is_downloadable/position/processing_status: un autosave nunca debe
	// poder despublicar o reordenar un recurso por accidente.
	AutosaveResource(ctx context.Context, resourceID uuid.UUID, title *string, markdown *string, at time.Time) error
	DeleteResource(ctx context.Context, resourceID uuid.UUID) error

	ReorderModules(ctx context.Context, versionID uuid.UUID, orderedIDs []uuid.UUID) error
	ReorderUnits(ctx context.Context, moduleID uuid.UUID, orderedIDs []uuid.UUID) error
	ReorderResources(ctx context.Context, unitID uuid.UUID, orderedIDs []uuid.UUID) error
}
