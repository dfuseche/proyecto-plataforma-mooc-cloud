package domain

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// fakeOwnershipRepo implementa lo mínimo de CourseRepository y CourseLocator
// para probar AuthorizeCourseEdit sin base de datos.
type fakeOwnershipRepo struct {
	CourseRepository
	course   *Course
	notFound bool
}

func (f *fakeOwnershipRepo) CourseIDOf(_ context.Context, _ CourseEntityKind, _ uuid.UUID) (uuid.UUID, error) {
	if f.notFound {
		return uuid.Nil, ErrCourseNotFound
	}
	return f.course.ID, nil
}

func (f *fakeOwnershipRepo) GetCourseByID(_ context.Context, _ uuid.UUID) (*Course, error) {
	return f.course, nil
}

// repoWithoutLocator no sabe localizar cursos: la autorización debe fallar cerrada.
type repoWithoutLocator struct{ CourseRepository }

func TestAuthorizeCourseEdit(t *testing.T) {
	owner := &User{ID: uuid.New(), Role: RoleTeacher}
	other := &User{ID: uuid.New(), Role: RoleTeacher}
	admin := &User{ID: uuid.New(), Role: RoleAdmin}
	student := &User{ID: uuid.New(), Role: RoleStudent}
	repo := &fakeOwnershipRepo{course: &Course{ID: uuid.New(), CreatedByTeacherID: owner.ID}}

	cases := []struct {
		name string
		repo CourseRepository
		user *User
		want error
	}{
		{"el dueño puede editar", repo, owner, nil},
		{"un profesor ajeno no puede editar", repo, other, ErrUnauthorizedCourseMutation},
		{"el admin puede editar cualquier curso", repo, admin, nil},
		{"un estudiante no puede editar", repo, student, ErrUnauthorizedCourseMutation},
		{"sin usuario se rechaza", repo, nil, ErrUnauthorizedCourseMutation},
		{"entidad inexistente da not found", &fakeOwnershipRepo{notFound: true, course: repo.course}, owner, ErrCourseNotFound},
		{"repositorio sin locator falla cerrado", repoWithoutLocator{}, admin, ErrUnauthorizedCourseMutation},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AuthorizeCourseEdit(context.Background(), tc.repo, tc.user, EntityResource, uuid.New())
			if got != tc.want {
				t.Fatalf("esperaba %v, obtuve %v", tc.want, got)
			}
		})
	}
}
