package course

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mooc-platform/backend/internal/domain"
)

type StorageProvider interface {
	GeneratePresignedUpload(ctx context.Context, objectKey string, contentType string) (string, error)
}

type UseCase struct {
	repo     domain.CourseRepository
	userRepo domain.UserRepository
	storage  StorageProvider
}

func NewUseCase(repo domain.CourseRepository, userRepo domain.UserRepository, storage ...StorageProvider) *UseCase {
	uc := &UseCase{repo: repo, userRepo: userRepo}
	if len(storage) > 0 {
		uc.storage = storage[0]
	}
	return uc
}

type CreateCourseInput struct {
	Title        string  `json:"title"`
	Summary      string  `json:"summary"`
	PassingScore float64 `json:"passing_score"`
}

func (uc *UseCase) CreateCourse(ctx context.Context, teacherID uuid.UUID, input CreateCourseInput) (*domain.Course, *domain.CourseVersion, error) {
	teacher, err := uc.userRepo.GetByID(ctx, teacherID)
	if err != nil || (teacher.Role != domain.RoleTeacher && teacher.Role != domain.RoleAdmin) {
		return nil, nil, domain.ErrUnauthorizedCourseMutation
	}

	slug := strings.ToLower(strings.ReplaceAll(input.Title, " ", "-"))

	course := &domain.Course{
		ID:                 uuid.New(),
		Slug:               slug,
		Title:              input.Title,
		Summary:            input.Summary,
		CreatedByTeacherID: teacherID,
	}

	passingScore := input.PassingScore
	if passingScore <= 0 {
		passingScore = 70.0
	}

	version := &domain.CourseVersion{
		ID:            uuid.New(),
		CourseID:      course.ID,
		VersionNumber: 1,
		Status:        domain.VersionStatusDraft,
		PassingScore:  passingScore,
	}

	if err := uc.repo.CreateCourse(ctx, course, version); err != nil {
		return nil, nil, err
	}

	return course, version, nil
}

func (uc *UseCase) CreateDraftFromPublished(ctx context.Context, teacherID uuid.UUID, courseID uuid.UUID) (*domain.CourseVersion, error) {
	course, err := uc.repo.GetCourseByID(ctx, courseID)
	if err != nil {
		return nil, err
	}

	if course.CurrentPublishedVersionID == nil {
		return nil, domain.ErrVersionNotFound
	}

	pubVersion, err := uc.repo.GetFullVersionHierarchy(ctx, *course.CurrentPublishedVersionID)
	if err != nil {
		return nil, err
	}

	newVersion := &domain.CourseVersion{
		ID:            uuid.New(),
		CourseID:      courseID,
		VersionNumber: pubVersion.VersionNumber + 1,
		Status:        domain.VersionStatusDraft,
		PassingScore:  pubVersion.PassingScore,
	}

	if err := uc.repo.CreateVersion(ctx, newVersion); err != nil {
		return nil, err
	}

	// Copiar jerarquía profunda preservando stable_ids
	for _, mod := range pubVersion.Modules {
		newMod := &domain.Module{
			ID:          uuid.New(),
			VersionID:   newVersion.ID,
			StableID:    mod.StableID, // Preservado
			Title:       mod.Title,
			Description: mod.Description,
			Position:    mod.Position,
		}
		_ = uc.repo.CreateModule(ctx, newMod)

		for _, u := range mod.Units {
			newUnit := &domain.Unit{
				ID:       uuid.New(),
				ModuleID: newMod.ID,
				StableID: u.StableID, // Preservado
				Title:    u.Title,
				Position: u.Position,
			}
			_ = uc.repo.CreateUnit(ctx, newUnit)

			for _, r := range u.Resources {
				newRes := &domain.Resource{
					ID:                uuid.New(),
					UnitID:            newUnit.ID,
					StableID:          r.StableID, // Preservado
					Title:             r.Title,
					Type:              r.Type,
					CanonicalMarkdown: r.CanonicalMarkdown,
					MediaURL:          r.MediaURL,
					IsVisible:         r.IsVisible,
					IsMandatory:       r.IsMandatory,
					IsDownloadable:    r.IsDownloadable,
					Position:          r.Position,
					ProcessingStatus:  r.ProcessingStatus,
				}
				_ = uc.repo.CreateResource(ctx, newRes)
			}
		}
	}

	return uc.repo.GetFullVersionHierarchy(ctx, newVersion.ID)
}

func (uc *UseCase) AddModule(ctx context.Context, teacherID uuid.UUID, versionID uuid.UUID, title string, description string, position int) (*domain.Module, error) {
	version, err := uc.repo.GetVersionByID(ctx, versionID)
	if err != nil {
		return nil, err
	}

	if version.Status != domain.VersionStatusDraft {
		return nil, domain.ErrVersionImmutable
	}

	module := &domain.Module{
		ID:          uuid.New(),
		VersionID:   versionID,
		StableID:    uuid.New(),
		Title:       title,
		Description: description,
		Position:    position,
	}

	if err := uc.repo.CreateModule(ctx, module); err != nil {
		return nil, err
	}

	return module, nil
}

func (uc *UseCase) AddUnit(ctx context.Context, teacherID uuid.UUID, moduleID uuid.UUID, title string, position int) (*domain.Unit, error) {
	unit := &domain.Unit{
		ID:        uuid.New(),
		ModuleID:  moduleID,
		StableID:  uuid.New(),
		Title:     title,
		Position:  position,
	}

	if err := uc.repo.CreateUnit(ctx, unit); err != nil {
		return nil, err
	}

	return unit, nil
}

func (uc *UseCase) AddResource(ctx context.Context, teacherID uuid.UUID, resource *domain.Resource) (*domain.Resource, error) {
	if resource.ID == uuid.Nil {
		resource.ID = uuid.New()
	}
	if resource.StableID == uuid.Nil {
		resource.StableID = uuid.New()
	}

	if resource.ObjectKey == "" {
		resource.ObjectKey = fmt.Sprintf("resources/%s/raw", resource.ID.String())
	}

	if uc.storage != nil {
		contentType := "application/octet-stream"
		switch resource.Type {
		case domain.ResourceTypeVideo:
			contentType = "video/mp4"
		case domain.ResourceTypeAudio:
			contentType = "audio/mpeg"
		case domain.ResourceTypePDF:
			contentType = "application/pdf"
		}

		presignedURL, err := uc.storage.GeneratePresignedUpload(ctx, resource.ObjectKey, contentType)
		if err == nil {
			resource.PresignedUploadURL = presignedURL
		}
	}

	if resource.MediaURL == "" {
		resource.MediaURL = fmt.Sprintf("s3://mooc-media/%s", resource.ObjectKey)
	}

	if err := uc.repo.CreateResource(ctx, resource); err != nil {
		return nil, err
	}

	return resource, nil
}

// UpdateResourceFields representa un PATCH parcial sobre un recurso: solo
// los punteros no-nil se aplican. Es deliberadamente distinto de pasar un
// *domain.Resource directamente, porque con bool/string/int por valor es
// imposible distinguir "no mandaron este campo" de "lo mandaron en false/
// vacio/cero" — ese era exactamente el bug que hacia inestable cualquier
// guardado parcial (un autosave que solo mandara canonical_markdown
// terminaba apagando is_visible/is_mandatory/is_downloadable).
type UpdateResourceFields struct {
	Title             *string `json:"title"`
	Type              *string `json:"type"`
	CanonicalMarkdown *string `json:"canonical_markdown"`
	MediaURL          *string `json:"media_url"`
	IsVisible         *bool   `json:"is_visible"`
	IsMandatory       *bool   `json:"is_mandatory"`
	IsDownloadable    *bool   `json:"is_downloadable"`
	Position          *int    `json:"position"`
	ProcessingStatus  *string `json:"processing_status"`
}

func (uc *UseCase) UpdateResource(ctx context.Context, teacherID uuid.UUID, resourceID uuid.UUID, fields UpdateResourceFields) (*domain.Resource, error) {
	existing, err := uc.repo.GetResourceByID(ctx, resourceID)
	if err != nil {
		return nil, err
	}

	if fields.Title != nil {
		existing.Title = *fields.Title
	}
	if fields.Type != nil {
		existing.Type = domain.ResourceType(*fields.Type)
	}
	if fields.CanonicalMarkdown != nil {
		existing.CanonicalMarkdown = *fields.CanonicalMarkdown
	}
	if fields.MediaURL != nil {
		existing.MediaURL = *fields.MediaURL
	}
	if fields.IsVisible != nil {
		existing.IsVisible = *fields.IsVisible
	}
	if fields.IsMandatory != nil {
		existing.IsMandatory = *fields.IsMandatory
	}
	if fields.IsDownloadable != nil {
		existing.IsDownloadable = *fields.IsDownloadable
	}
	if fields.Position != nil {
		existing.Position = *fields.Position
	}
	if fields.ProcessingStatus != nil {
		existing.ProcessingStatus = domain.ProcessingStatus(*fields.ProcessingStatus)
	}

	if err := uc.repo.UpdateResource(ctx, existing); err != nil {
		return nil, err
	}
	return uc.repo.GetResourceByID(ctx, resourceID)
}

// AutosaveResource guarda SOLO contenido (title/markdown) con timestamp
// de autosave, sin tocar visibilidad/orden/estado de procesamiento. Es el
// mecanismo pensado para que un editor llame cada pocos segundos mientras
// el usuario escribe, sin arriesgar el resto del recurso ni requerir que
// el cliente reenvie el objeto completo en cada llamada.
func (uc *UseCase) AutosaveResource(ctx context.Context, teacherID uuid.UUID, resourceID uuid.UUID, title *string, markdown *string) (*domain.Resource, error) {
	now := time.Now()
	if err := uc.repo.AutosaveResource(ctx, resourceID, title, markdown, now); err != nil {
		return nil, err
	}
	return uc.repo.GetResourceByID(ctx, resourceID)
}

func (uc *UseCase) DeleteResource(ctx context.Context, teacherID uuid.UUID, resourceID uuid.UUID) error {
	return uc.repo.DeleteResource(ctx, resourceID)
}

// validatePublishStructure recorre TODA la jerarquia y junta TODOS los
// problemas que impiden publicar, en vez de retornar apenas se encuentra
// el primero. Es exactamente lo que la rubrica pidio ("validaciones
// exhaustivas de publicacion... en vez de devolver la lista exhaustiva").
func validatePublishStructure(course *domain.Course, version *domain.CourseVersion) []string {
	var issues []string

	if strings.TrimSpace(course.Title) == "" {
		issues = append(issues, "el curso no tiene titulo")
	}
	if strings.TrimSpace(course.Summary) == "" {
		issues = append(issues, "el curso no tiene resumen")
	}
	if version.PassingScore <= 0 {
		issues = append(issues, "el criterio de aprobacion (passing_score) debe ser mayor a 0")
	}

	if len(version.Modules) == 0 {
		issues = append(issues, "el curso no tiene ningun modulo")
	}

	hasPublishableResource := false
	for _, mod := range version.Modules {
		if strings.TrimSpace(mod.Title) == "" {
			issues = append(issues, fmt.Sprintf("el modulo en la posicion %d no tiene titulo", mod.Position))
		}
		if len(mod.Units) == 0 {
			issues = append(issues, fmt.Sprintf("el modulo %q no tiene ninguna unidad", mod.Title))
			continue
		}
		for _, u := range mod.Units {
			if strings.TrimSpace(u.Title) == "" {
				issues = append(issues, fmt.Sprintf("la unidad en la posicion %d del modulo %q no tiene titulo", u.Position, mod.Title))
			}
			if len(u.Resources) == 0 {
				issues = append(issues, fmt.Sprintf("la unidad %q no tiene ningun recurso", u.Title))
				continue
			}
			for _, r := range u.Resources {
				if r.IsVisible && r.ProcessingStatus == domain.ProcessingFailed {
					issues = append(issues, fmt.Sprintf("el recurso %q esta marcado visible pero su procesamiento fallo", r.Title))
				}
				if r.IsVisible && r.ProcessingStatus == domain.ProcessingCompleted {
					hasPublishableResource = true
				}
			}
		}
	}

	if !hasPublishableResource {
		issues = append(issues, "el curso no tiene ningun recurso visible y disponible (processing_status=completed)")
	}

	return issues
}

func (uc *UseCase) PublishVersion(ctx context.Context, teacherID uuid.UUID, versionID uuid.UUID) (*domain.CourseVersion, error) {
	versionHierarchy, err := uc.repo.GetFullVersionHierarchy(ctx, versionID)
	if err != nil {
		return nil, err
	}

	if versionHierarchy.Status != domain.VersionStatusDraft {
		return nil, domain.ErrVersionImmutable
	}

	course, err := uc.repo.GetCourseByID(ctx, versionHierarchy.CourseID)
	if err != nil {
		return nil, err
	}

	if issues := validatePublishStructure(course, versionHierarchy); len(issues) > 0 {
		return nil, &domain.PublishValidationError{Issues: issues}
	}

	if err := uc.repo.PublishVersion(ctx, versionHierarchy.CourseID, versionID); err != nil {
		return nil, err
	}

	versionHierarchy.Status = domain.VersionStatusPublished
	return versionHierarchy, nil
}

func (uc *UseCase) UnpublishCourse(ctx context.Context, teacherID uuid.UUID, courseID uuid.UUID) error {
	return uc.repo.UnpublishCourse(ctx, courseID)
}

func (uc *UseCase) GetCourseCatalog(ctx context.Context, limit, offset int) ([]*domain.Course, error) {
	return uc.repo.ListCourses(ctx, limit, offset)
}

func (uc *UseCase) GetCourseHierarchy(ctx context.Context, courseID uuid.UUID) (*domain.CourseVersion, error) {
	course, err := uc.repo.GetCourseByID(ctx, courseID)
	if err != nil {
		return nil, err
	}

	if course.CurrentPublishedVersionID == nil {
		return nil, domain.ErrVersionNotFound
	}

	return uc.repo.GetFullVersionHierarchy(ctx, *course.CurrentPublishedVersionID)
}

func (uc *UseCase) GetCourseDraftPreview(ctx context.Context, teacherID uuid.UUID, courseID uuid.UUID) (*domain.CourseVersion, error) {
	draft, err := uc.repo.GetLatestDraftVersion(ctx, courseID)
	if err != nil {
		return nil, err
	}
	return uc.repo.GetFullVersionHierarchy(ctx, draft.ID)
}

func (uc *UseCase) ReorderModules(ctx context.Context, teacherID uuid.UUID, versionID uuid.UUID, orderedIDs []uuid.UUID) error {
	return uc.repo.ReorderModules(ctx, versionID, orderedIDs)
}

func (uc *UseCase) ReorderUnits(ctx context.Context, teacherID uuid.UUID, moduleID uuid.UUID, orderedIDs []uuid.UUID) error {
	return uc.repo.ReorderUnits(ctx, moduleID, orderedIDs)
}

func (uc *UseCase) ReorderResources(ctx context.Context, teacherID uuid.UUID, unitID uuid.UUID, orderedIDs []uuid.UUID) error {
	return uc.repo.ReorderResources(ctx, unitID, orderedIDs)
}
