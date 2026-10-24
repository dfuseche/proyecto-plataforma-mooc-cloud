package domain

import (
	"context"

	"github.com/google/uuid"
)

// CourseEntityKind identifica qué tipo de id se usa para localizar el curso
// al que pertenece una entidad de autoría.
type CourseEntityKind string

const (
	EntityCourse   CourseEntityKind = "course"
	EntityVersion  CourseEntityKind = "version"
	EntityModule   CourseEntityKind = "module"
	EntityUnit     CourseEntityKind = "unit"
	EntityResource CourseEntityKind = "resource"
)

// CourseLocator es una capacidad opcional de los repositorios de cursos:
// resuelve a qué curso pertenece una versión, módulo, unidad o recurso. Se
// consulta por type assertion para no ampliar CourseRepository (ni sus mocks).
// Debe devolver ErrCourseNotFound si la entidad no existe.
type CourseLocator interface {
	CourseIDOf(ctx context.Context, kind CourseEntityKind, id uuid.UUID) (uuid.UUID, error)
}

// AuthorizeCourseEdit comprueba que user puede modificar el curso al que
// pertenece la entidad indicada: el administrador siempre; un profesor solo
// si es quien creó el curso. Falla cerrado: si el repositorio no sabe localizar
// el curso, la operación se rechaza.
func AuthorizeCourseEdit(ctx context.Context, repo CourseRepository, user *User, kind CourseEntityKind, id uuid.UUID) error {
	if user == nil {
		return ErrUnauthorizedCourseMutation
	}
	locator, ok := repo.(CourseLocator)
	if !ok {
		return ErrUnauthorizedCourseMutation
	}
	courseID, err := locator.CourseIDOf(ctx, kind, id)
	if err != nil {
		return err
	}
	if user.Role == RoleAdmin {
		return nil
	}
	course, err := repo.GetCourseByID(ctx, courseID)
	if err != nil {
		return err
	}
	if user.Role != RoleTeacher || course.CreatedByTeacherID != user.ID {
		return ErrUnauthorizedCourseMutation
	}
	return nil
}
