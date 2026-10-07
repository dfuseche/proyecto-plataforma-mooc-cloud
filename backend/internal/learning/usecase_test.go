package learning

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mooc-platform/backend/internal/domain"
)

type mockLearningRepo struct {
	enrollments map[string]*domain.Enrollment
	quizzes     map[uuid.UUID]*domain.Quiz
	attempts    map[uuid.UUID]*domain.QuizAttempt
	progress    map[string]*domain.ResourceProgress
	badges      map[string]*domain.Badge
}

func newMockLearningRepo() *mockLearningRepo {
	return &mockLearningRepo{
		enrollments: make(map[string]*domain.Enrollment),
		quizzes:     make(map[uuid.UUID]*domain.Quiz),
		attempts:    make(map[uuid.UUID]*domain.QuizAttempt),
		progress:    make(map[string]*domain.ResourceProgress),
		badges:      make(map[string]*domain.Badge),
	}
}

func (m *mockLearningRepo) EnrollStudent(ctx context.Context, e *domain.Enrollment) error {
	key := e.StudentID.String() + ":" + e.CourseID.String()
	m.enrollments[key] = e
	return nil
}

func (m *mockLearningRepo) GetEnrollment(ctx context.Context, studentID, courseID uuid.UUID) (*domain.Enrollment, error) {
	key := studentID.String() + ":" + courseID.String()
	e, ok := m.enrollments[key]
	if !ok {
		return nil, domain.ErrNotEnrolled
	}
	return e, nil
}

func (m *mockLearningRepo) UpdateEnrollment(ctx context.Context, e *domain.Enrollment) error {
	key := e.StudentID.String() + ":" + e.CourseID.String()
	m.enrollments[key] = e
	return nil
}

func (m *mockLearningRepo) CreateQuiz(ctx context.Context, q *domain.Quiz) error {
	m.quizzes[q.ID] = q
	return nil
}

func (m *mockLearningRepo) GetQuizByResourceID(ctx context.Context, resourceID uuid.UUID) (*domain.Quiz, error) {
	for _, q := range m.quizzes {
		if q.ResourceID == resourceID {
			return q, nil
		}
	}
	return nil, domain.ErrCourseNotFound
}

func (m *mockLearningRepo) GetQuizWithAnswers(ctx context.Context, quizID uuid.UUID) (*domain.Quiz, error) {
	q, ok := m.quizzes[quizID]
	if !ok {
		return nil, domain.ErrCourseNotFound
	}
	return q, nil
}

func (m *mockLearningRepo) CreateQuizAttempt(ctx context.Context, a *domain.QuizAttempt) error {
	m.attempts[a.ID] = a
	return nil
}

func (m *mockLearningRepo) GetAttemptByID(ctx context.Context, attemptID uuid.UUID) (*domain.QuizAttempt, error) {
	a, ok := m.attempts[attemptID]
	if !ok {
		return nil, domain.ErrCourseNotFound
	}
	return a, nil
}

func (m *mockLearningRepo) GetStudentAttemptsCount(ctx context.Context, studentID, quizID uuid.UUID) (int, error) {
	count := 0
	for _, a := range m.attempts {
		if a.StudentID == studentID && a.QuizID == quizID {
			count++
		}
	}
	return count, nil
}

func (m *mockLearningRepo) UpdateQuizAttempt(ctx context.Context, a *domain.QuizAttempt) error {
	m.attempts[a.ID] = a
	return nil
}

func (m *mockLearningRepo) UpsertResourceProgress(ctx context.Context, p *domain.ResourceProgress) error {
	key := p.StudentID.String() + ":" + p.CourseID.String() + ":" + p.ResourceStableID.String()
	m.progress[key] = p
	return nil
}

func (m *mockLearningRepo) GetStudentCourseProgress(ctx context.Context, studentID, courseID uuid.UUID) ([]*domain.ResourceProgress, error) {
	prefix := studentID.String() + ":" + courseID.String() + ":"
	list := make([]*domain.ResourceProgress, 0)
	for k, p := range m.progress {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			list = append(list, p)
		}
	}
	return list, nil
}

func (m *mockLearningRepo) CreateBadge(ctx context.Context, b *domain.Badge) error {
	key := b.StudentID.String() + ":" + b.CourseID.String()
	m.badges[key] = b
	return nil
}

func (m *mockLearningRepo) GetBadgeByCode(ctx context.Context, code uuid.UUID) (*domain.Badge, error) {
	for _, b := range m.badges {
		if b.VerificationCode == code {
			return b, nil
		}
	}
	return nil, domain.ErrCourseNotFound
}

func (m *mockLearningRepo) GetStudentBadgeForCourse(ctx context.Context, studentID, courseID uuid.UUID) (*domain.Badge, error) {
	key := studentID.String() + ":" + courseID.String()
	b, ok := m.badges[key]
	if !ok {
		return nil, domain.ErrCourseNotFound
	}
	return b, nil
}

func (m *mockLearningRepo) GetActiveAttempt(ctx context.Context, studentID, quizID uuid.UUID) (*domain.QuizAttempt, error) {
	for _, a := range m.attempts {
		if a.StudentID == studentID && a.QuizID == quizID && a.Status == domain.AttemptInProgress {
			return a, nil
		}
	}
	return nil, nil
}

func (m *mockLearningRepo) GetStudentSubmittedAttemptsCount(ctx context.Context, studentID, quizID uuid.UUID) (int, error) {
	count := 0
	for _, a := range m.attempts {
		if a.StudentID == studentID && a.QuizID == quizID && a.Status == domain.AttemptSubmitted {
			count++
		}
	}
	return count, nil
}

func (m *mockLearningRepo) GetBadgeByID(ctx context.Context, badgeID uuid.UUID) (*domain.Badge, error) {
	for _, b := range m.badges {
		if b.ID == badgeID {
			return b, nil
		}
	}
	return nil, domain.ErrCourseNotFound
}

func (m *mockLearningRepo) RevokeBadge(ctx context.Context, badgeID uuid.UUID) error {
	for _, b := range m.badges {
		if b.ID == badgeID {
			b.IsRevoked = true
		}
	}
	return nil
}

type mockCourseRepo struct {
	course  *domain.Course
	version *domain.CourseVersion
}
func (c *mockCourseRepo) CreateCourse(ctx context.Context, course *domain.Course, initialVersion *domain.CourseVersion) error { return nil }
func (c *mockCourseRepo) GetCourseByID(ctx context.Context, id uuid.UUID) (*domain.Course, error) { return c.course, nil }
func (c *mockCourseRepo) GetCourseBySlug(ctx context.Context, slug string) (*domain.Course, error) { return c.course, nil }
func (c *mockCourseRepo) ListCourses(ctx context.Context, limit, offset int) ([]*domain.Course, error) { return nil, nil }
func (c *mockCourseRepo) UpdateCourse(ctx context.Context, course *domain.Course) error { return nil }
func (c *mockCourseRepo) CreateVersion(ctx context.Context, version *domain.CourseVersion) error { return nil }
func (c *mockCourseRepo) GetVersionByID(ctx context.Context, versionID uuid.UUID) (*domain.CourseVersion, error) { return c.version, nil }
func (c *mockCourseRepo) GetFullVersionHierarchy(ctx context.Context, versionID uuid.UUID) (*domain.CourseVersion, error) { return c.version, nil }
func (c *mockCourseRepo) PublishVersion(ctx context.Context, courseID uuid.UUID, versionID uuid.UUID) error { return nil }
func (c *mockCourseRepo) CreateModule(ctx context.Context, module *domain.Module) error { return nil }
func (c *mockCourseRepo) CreateUnit(ctx context.Context, unit *domain.Unit) error { return nil }
func (c *mockCourseRepo) CreateResource(ctx context.Context, resource *domain.Resource) error { return nil }
func (c *mockCourseRepo) GetResourceByID(ctx context.Context, resourceID uuid.UUID) (*domain.Resource, error) { return nil, nil }
func (c *mockCourseRepo) UpdateResource(ctx context.Context, resource *domain.Resource) error { return nil }
func (c *mockCourseRepo) AutosaveResource(ctx context.Context, resourceID uuid.UUID, title *string, markdown *string, at time.Time) error { return nil }
func (c *mockCourseRepo) DeleteResource(ctx context.Context, resourceID uuid.UUID) error { return nil }
func (c *mockCourseRepo) UnpublishCourse(ctx context.Context, courseID uuid.UUID) error { return nil }
func (c *mockCourseRepo) GetLatestDraftVersion(ctx context.Context, courseID uuid.UUID) (*domain.CourseVersion, error) { return nil, nil }
func (c *mockCourseRepo) ReorderModules(ctx context.Context, versionID uuid.UUID, orderedIDs []uuid.UUID) error { return nil }
func (c *mockCourseRepo) ReorderUnits(ctx context.Context, moduleID uuid.UUID, orderedIDs []uuid.UUID) error { return nil }
func (c *mockCourseRepo) ReorderResources(ctx context.Context, unitID uuid.UUID, orderedIDs []uuid.UUID) error { return nil }

type mockUserRepo struct{}
func (u *mockUserRepo) CreateUser(ctx context.Context, user *domain.User) error { return nil }
func (u *mockUserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) { return nil, nil }
func (u *mockUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) { return nil, nil }
func (u *mockUserRepo) UpdateUser(ctx context.Context, user *domain.User) error { return nil }
func (u *mockUserRepo) CountActiveAdmins(ctx context.Context) (int, error) { return 1, nil }
func (u *mockUserRepo) CreateSession(ctx context.Context, session *domain.UserSession) error { return nil }
func (u *mockUserRepo) GetSessionByToken(ctx context.Context, token string) (*domain.UserSession, error) { return nil, nil }
func (u *mockUserRepo) RevokeSession(ctx context.Context, sessionID uuid.UUID) error { return nil }
func (u *mockUserRepo) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error { return nil }
func (u *mockUserRepo) CreateToken(ctx context.Context, token *domain.UserToken) error { return nil }
func (u *mockUserRepo) GetToken(ctx context.Context, tokenStr string, tokenType string) (*domain.UserToken, error) { return nil, nil }
func (u *mockUserRepo) MarkTokenUsed(ctx context.Context, id uuid.UUID) error { return nil }
func (u *mockUserRepo) CreateAuditLog(ctx context.Context, log *domain.AuditLog) error { return nil }
func (u *mockUserRepo) ListUsers(ctx context.Context, role *domain.Role, status *domain.UserStatus, search string, limit, offset int) ([]*domain.User, int, error) { return nil, 0, nil }
func (u *mockUserRepo) ListAuditLogs(ctx context.Context, limit, offset int) ([]*domain.AuditLog, int, error) { return nil, 0, nil }

func TestServerSideQuizGrading(t *testing.T) {
	lRepo := newMockLearningRepo()
	cRepo := &mockCourseRepo{}
	uRepo := &mockUserRepo{}
	uc := NewUseCase(lRepo, cRepo, uRepo, "http://localhost:8080")
	ctx := context.Background()

	qID := uuid.New()
	q1ID := uuid.New()
	q1OptCorrect := uuid.New()
	q1OptWrong := uuid.New()

	quiz := &domain.Quiz{
		ID:           qID,
		MaxAttempts:  2,
		PassingScore: 70.0,
		Questions: []domain.QuizQuestion{
			{
				ID:           q1ID,
				QuizID:       qID,
				QuestionText: "¿Qué puerto usa Redis por defecto?",
				Points:       10.0,
				Options: []domain.QuizOption{
					{ID: q1OptCorrect, QuestionID: q1ID, OptionText: "6379", IsCorrect: true},
					{ID: q1OptWrong, QuestionID: q1ID, OptionText: "5432", IsCorrect: false},
				},
			},
		},
	}
	if err := lRepo.CreateQuiz(ctx, quiz); err != nil {
		t.Fatalf("setup: CreateQuiz falló: %v", err)
	}

	studentID := uuid.New()

	// 1. Envío con respuesta correcta
	attempt1, err := uc.SubmitQuizAttempt(ctx, studentID, qID, nil, map[string]string{
		q1ID.String(): q1OptCorrect.String(),
	})
	if err != nil {
		t.Fatalf("Intento 1 de quiz falló: %v", err)
	}
	if *attempt1.Score != 100.0 {
		t.Errorf("Esperaba puntaje 100.0, obtenido: %.2f", *attempt1.Score)
	}

	// 2. Envío con respuesta incorrecta
	attempt2, err := uc.SubmitQuizAttempt(ctx, studentID, qID, nil, map[string]string{
		q1ID.String(): q1OptWrong.String(),
	})
	if err != nil {
		t.Fatalf("Intento 2 de quiz falló: %v", err)
	}
	if *attempt2.Score != 0.0 {
		t.Errorf("Esperaba puntaje 0.0, obtenido: %.2f", *attempt2.Score)
	}

	// 3. Superar el número máximo de intentos (debe fallar)
	_, err = uc.SubmitQuizAttempt(ctx, studentID, qID, nil, map[string]string{
		q1ID.String(): q1OptCorrect.String(),
	})
	if err != domain.ErrMaxAttemptsReached {
		t.Errorf("Esperaba ErrMaxAttemptsReached, obtenido: %v", err)
	}
}

func TestHeartbeatProgressAndBadgeIssuance(t *testing.T) {
	lRepo := newMockLearningRepo()
	studentID := uuid.New()
	courseID := uuid.New()
	versionID := uuid.New()
	stableID1 := uuid.New()

	course := &domain.Course{
		ID:                        courseID,
		CurrentPublishedVersionID: &versionID,
	}
	version := &domain.CourseVersion{
		ID:       versionID,
		CourseID: courseID,
		Modules: []domain.Module{
			{
				Units: []domain.Unit{
					{
						Resources: []domain.Resource{
							{StableID: stableID1, IsVisible: true, IsMandatory: true},
						},
					},
				},
			},
		},
	}

	cRepo := &mockCourseRepo{course: course, version: version}
	uRepo := &mockUserRepo{}
	uc := NewUseCase(lRepo, cRepo, uRepo, "http://localhost:8080")
	ctx := context.Background()

	// Inscribir estudiante
	_, _ = uc.EnrollStudent(ctx, studentID, courseID)

	// Registrar Heartbeat con permanencia >= 10s
	enrollment, err := uc.RecordHeartbeat(ctx, HeartbeatInput{
		StudentID:        studentID,
		CourseID:         courseID,
		ResourceStableID: stableID1,
		DwellTimeSeconds: 15,
	})
	if err != nil {
		t.Fatalf("RecordHeartbeat falló: %v", err)
	}

	if enrollment.ProgressPercentage != 100.0 {
		t.Errorf("Esperaba avance 100%%, obtenido: %.2f", enrollment.ProgressPercentage)
	}
	if enrollment.AcademicStatus != domain.AcademicApproved {
		t.Errorf("Esperaba estado de aprobación, obtenido: %s", enrollment.AcademicStatus)
	}

	// Verificar emisión de insignia idempotente
	badge, err := lRepo.GetStudentBadgeForCourse(ctx, studentID, courseID)
	if err != nil {
		t.Fatalf("Insignia no fue emitida en base de datos: %v", err)
	}
	if badge.VerificationURL == "" {
		t.Error("Esperaba URL pública de verificación de la insignia")
	}
}

func TestFullQuizLifecycle(t *testing.T) {
	lRepo := newMockLearningRepo()
	cRepo := &mockCourseRepo{}
	uRepo := &mockUserRepo{}
	uc := NewUseCase(lRepo, cRepo, uRepo, "http://localhost:8080")
	ctx := context.Background()

	resourceID := uuid.New()
	teacherID := uuid.New()
	studentID := uuid.New()

	// 1. Crear Quiz
	quizInput := &domain.Quiz{
		ResourceID:   resourceID,
		MaxAttempts:  1,
		PassingScore: 70.0,
		Questions: []domain.QuizQuestion{
			{
				QuestionText: "¿Qué es Go?",
				Position:     1,
				Points:       100.0,
				Options: []domain.QuizOption{
					{OptionText: "Un lenguaje de programación", IsCorrect: true, Position: 1},
					{OptionText: "Un motor de base de datos", IsCorrect: false, Position: 2},
				},
			},
		},
	}

	createdQuiz, err := uc.CreateQuiz(ctx, teacherID, quizInput)
	if err != nil {
		t.Fatalf("CreateQuiz falló: %v", err)
	}

	if createdQuiz.ID == uuid.Nil {
		t.Fatal("Quiz ID no debería ser Nil")
	}
	if len(createdQuiz.Questions) != 1 || createdQuiz.Questions[0].ID == uuid.Nil {
		t.Fatal("Question ID no debería ser Nil tras CreateQuiz")
	}
	optCorrect := createdQuiz.Questions[0].Options[0]
	if optCorrect.ID == uuid.Nil {
		t.Fatal("Option ID no debería ser Nil tras CreateQuiz")
	}
	if !optCorrect.IsCorrect {
		t.Fatal("Option IsCorrect debería ser true tras unmarshaling/creación")
	}

	// 2. Obtener Snapshot Seguro
	snapshot, err := uc.GetQuizSnapshot(ctx, createdQuiz.ID)
	if err != nil {
		t.Fatalf("GetQuizSnapshot falló: %v", err)
	}
	if len(snapshot.Questions) != 1 {
		t.Fatalf("Snapshot debería contener 1 pregunta")
	}

	// 3. Iniciar Intento
	attempt, err := uc.StartQuizAttempt(ctx, studentID, createdQuiz.ID)
	if err != nil {
		t.Fatalf("StartQuizAttempt falló: %v", err)
	}
	if attempt.Status != domain.AttemptInProgress {
		t.Fatalf("El estado del intento debería ser in_progress, obtenido: %s", attempt.Status)
	}

	// 4. Iniciar Intento de nuevo (debería retornar el mismo intento activo sin fallar)
	activeAtt, err := uc.StartQuizAttempt(ctx, studentID, createdQuiz.ID)
	if err != nil {
		t.Fatalf("Re-StartQuizAttempt falló: %v", err)
	}
	if activeAtt.ID != attempt.ID {
		t.Fatalf("Re-StartQuizAttempt debería retornar el mismo intento activo")
	}

	// 5. Guardar Avance Parcial
	questionIDStr := createdQuiz.Questions[0].ID.String()
	_, err = uc.SavePartialAttempt(ctx, studentID, attempt.ID, map[string]string{
		questionIDStr: optCorrect.ID.String(),
	})
	if err != nil {
		t.Fatalf("SavePartialAttempt falló: %v", err)
	}

	// 6. Enviar Intento para Calificación
	submittedAttempt, err := uc.SubmitQuizAttempt(ctx, studentID, createdQuiz.ID, &attempt.ID, map[string]string{
		questionIDStr: optCorrect.ID.String(),
	})
	if err != nil {
		t.Fatalf("SubmitQuizAttempt falló: %v", err)
	}

	if submittedAttempt.Status != domain.AttemptSubmitted {
		t.Fatalf("El estado del intento debería ser submitted")
	}
	if *submittedAttempt.Score != 100.0 {
		t.Fatalf("Esperaba score 100.0, obtenido: %.2f", *submittedAttempt.Score)
	}

	// 7. Intentar nuevo intento tras alcanzar MaxAttempts (1)
	_, err = uc.StartQuizAttempt(ctx, studentID, createdQuiz.ID)
	if err != domain.ErrMaxAttemptsReached {
		t.Fatalf("Esperaba ErrMaxAttemptsReached tras agotar intentos, obtenido: %v", err)
	}
}
