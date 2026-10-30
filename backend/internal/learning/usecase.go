package learning

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/mooc-platform/backend/internal/domain"
)

type UseCase struct {
	repo       domain.LearningRepository
	courseRepo domain.CourseRepository
	userRepo   domain.UserRepository
	baseURL    string
}

func NewUseCase(repo domain.LearningRepository, courseRepo domain.CourseRepository, userRepo domain.UserRepository, baseURL string) *UseCase {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	return &UseCase{
		repo:       repo,
		courseRepo: courseRepo,
		userRepo:   userRepo,
		baseURL:    baseURL,
	}
}

func (uc *UseCase) EnrollStudent(ctx context.Context, studentID, courseID uuid.UUID) (*domain.Enrollment, error) {
	_, err := uc.courseRepo.GetCourseByID(ctx, courseID)
	if err != nil {
		return nil, err
	}

	enrollment := &domain.Enrollment{
		ID:                 uuid.New(),
		StudentID:          studentID,
		CourseID:           courseID,
		Status:             domain.EnrollmentActive,
		AcademicStatus:     domain.AcademicEnrolled,
		ProgressPercentage: 0.0,
	}

	if err := uc.repo.EnrollStudent(ctx, enrollment); err != nil {
		return nil, err
	}

	return enrollment, nil
}

func (uc *UseCase) CreateQuiz(ctx context.Context, teacherID uuid.UUID, quiz *domain.Quiz) (*domain.Quiz, error) {
	if quiz.ID == uuid.Nil {
		quiz.ID = uuid.New()
	}
	if quiz.MaxAttempts <= 0 {
		quiz.MaxAttempts = 3
	}
	if quiz.PassingScore <= 0 {
		quiz.PassingScore = 70.0
	}

	for i := range quiz.Questions {
		if quiz.Questions[i].ID == uuid.Nil {
			quiz.Questions[i].ID = uuid.New()
		}
		quiz.Questions[i].QuizID = quiz.ID
		for j := range quiz.Questions[i].Options {
			if quiz.Questions[i].Options[j].ID == uuid.Nil {
				quiz.Questions[i].Options[j].ID = uuid.New()
			}
			quiz.Questions[i].Options[j].QuestionID = quiz.Questions[i].ID
		}
	}

	if err := uc.repo.CreateQuiz(ctx, quiz); err != nil {
		return nil, err
	}

	return quiz, nil
}

type QuizSnapshotResponse struct {
	ID           uuid.UUID                 `json:"id"`
	ResourceID   uuid.UUID                 `json:"resource_id"`
	MaxAttempts  int                       `json:"max_attempts"`
	PassingScore float64                   `json:"passing_score"`
	Questions    []QuizSnapshotQuestionDTO `json:"questions"`
}

type QuizSnapshotQuestionDTO struct {
	ID           uuid.UUID                  `json:"id"`
	QuestionText string                     `json:"question_text"`
	Position     int                        `json:"position"`
	Points       float64                    `json:"points"`
	Options      []domain.StudentQuizOption `json:"options"`
}

func (uc *UseCase) GetQuizSnapshot(ctx context.Context, quizID uuid.UUID) (*QuizSnapshotResponse, error) {
	quiz, err := uc.repo.GetQuizWithAnswers(ctx, quizID)
	if err != nil {
		return nil, err
	}

	questionsDTO := make([]QuizSnapshotQuestionDTO, 0)
	for _, quest := range quiz.Questions {
		optsDTO := make([]domain.StudentQuizOption, 0)
		for _, opt := range quest.Options {
			optsDTO = append(optsDTO, domain.StudentQuizOption{
				ID:         opt.ID,
				QuestionID: opt.QuestionID,
				OptionText: opt.OptionText,
				Position:   opt.Position,
			})
		}
		questionsDTO = append(questionsDTO, QuizSnapshotQuestionDTO{
			ID:           quest.ID,
			QuestionText: quest.QuestionText,
			Position:     quest.Position,
			Points:       quest.Points,
			Options:      optsDTO,
		})
	}

	return &QuizSnapshotResponse{
		ID:           quiz.ID,
		ResourceID:   quiz.ResourceID,
		MaxAttempts:  quiz.MaxAttempts,
		PassingScore: quiz.PassingScore,
		Questions:    questionsDTO,
	}, nil
}

func (uc *UseCase) StartQuizAttempt(ctx context.Context, studentID, quizID uuid.UUID) (*domain.QuizAttempt, error) {
	quiz, err := uc.repo.GetQuizWithAnswers(ctx, quizID)
	if err != nil {
		return nil, err
	}

	activeAttempt, err := uc.repo.GetActiveAttempt(ctx, studentID, quizID)
	if err == nil && activeAttempt != nil {
		return activeAttempt, nil
	}

	submittedCount, err := uc.repo.GetStudentSubmittedAttemptsCount(ctx, studentID, quizID)
	if err != nil {
		return nil, err
	}

	if submittedCount >= quiz.MaxAttempts {
		return nil, domain.ErrMaxAttemptsReached
	}

	attempt := &domain.QuizAttempt{
		ID:            uuid.New(),
		StudentID:     studentID,
		QuizID:        quizID,
		AttemptNumber: submittedCount + 1,
		Status:        domain.AttemptInProgress,
		Answers:       make(map[string]string),
	}

	if err := uc.repo.CreateQuizAttempt(ctx, attempt); err != nil {
		return nil, err
	}

	return attempt, nil
}

func (uc *UseCase) SavePartialAttempt(ctx context.Context, studentID, attemptID uuid.UUID, answers map[string]string) (*domain.QuizAttempt, error) {
	attempt, err := uc.repo.GetAttemptByID(ctx, attemptID)
	if err != nil {
		return nil, err
	}

	if attempt.StudentID != studentID {
		return nil, domain.ErrForbidden
	}

	if attempt.Status != domain.AttemptInProgress {
		return nil, domain.ErrAttemptAlreadySubmitted
	}

	if attempt.Answers == nil {
		attempt.Answers = make(map[string]string)
	}

	for k, v := range answers {
		attempt.Answers[k] = v
	}

	if err := uc.repo.UpdateQuizAttempt(ctx, attempt); err != nil {
		return nil, err
	}

	return attempt, nil
}

func (uc *UseCase) SubmitQuizAttempt(ctx context.Context, studentID, quizID uuid.UUID, attemptID *uuid.UUID, answers map[string]string) (*domain.QuizAttempt, error) {
	var attempt *domain.QuizAttempt

	if attemptID != nil && *attemptID != uuid.Nil {
		att, err := uc.repo.GetAttemptByID(ctx, *attemptID)
		if err != nil {
			return nil, err
		}
		if att.StudentID != studentID {
			return nil, domain.ErrForbidden
		}
		if att.Status == domain.AttemptSubmitted {
			return nil, domain.ErrAttemptAlreadySubmitted
		}
		attempt = att
		quizID = att.QuizID
	}

	quiz, err := uc.repo.GetQuizWithAnswers(ctx, quizID)
	if err != nil {
		return nil, err
	}

	if attempt == nil {
		att, err := uc.repo.GetActiveAttempt(ctx, studentID, quizID)
		if err == nil && att != nil {
			attempt = att
		}
	}

	submittedCount, err := uc.repo.GetStudentSubmittedAttemptsCount(ctx, studentID, quizID)
	if err != nil {
		return nil, err
	}

	if attempt == nil {
		if submittedCount >= quiz.MaxAttempts {
			return nil, domain.ErrMaxAttemptsReached
		}
		attempt = &domain.QuizAttempt{
			ID:            uuid.New(),
			StudentID:     studentID,
			QuizID:        quizID,
			AttemptNumber: submittedCount + 1,
			Answers:       make(map[string]string),
		}
	}

	if attempt.Answers == nil {
		attempt.Answers = make(map[string]string)
	}
	for k, v := range answers {
		attempt.Answers[k] = v
	}

	var totalPoints float64 = 0
	var earnedPoints float64 = 0

	for _, quest := range quiz.Questions {
		totalPoints += quest.Points

		selectedOptionIDStr, answered := attempt.Answers[quest.ID.String()]
		if !answered {
			continue
		}

		selectedOptionID, err := uuid.Parse(selectedOptionIDStr)
		if err != nil {
			continue
		}

		for _, opt := range quest.Options {
			if opt.ID == selectedOptionID && opt.IsCorrect {
				earnedPoints += quest.Points
				break
			}
		}
	}

	var score float64 = 0
	if totalPoints > 0 {
		score = (earnedPoints / totalPoints) * 100.0
	}

	now := time.Now()
	attempt.Status = domain.AttemptSubmitted
	attempt.Score = &score
	attempt.SubmittedAt = &now

	if err := uc.repo.UpdateQuizAttempt(ctx, attempt); err != nil {
		if err := uc.repo.CreateQuizAttempt(ctx, attempt); err != nil {
			return nil, err
		}
	}

	return attempt, nil
}

type HeartbeatInput struct {
	StudentID           uuid.UUID `json:"student_id"`
	CourseID            uuid.UUID `json:"course_id"`
	ResourceStableID    uuid.UUID `json:"resource_stable_id"`
	DwellTimeSeconds    int       `json:"dwell_time_seconds"`
	LastPositionSeconds int       `json:"last_position_seconds"`
}

// ResourceProgressItem es la vista mínima del avance en un recurso.
type ResourceProgressItem struct {
	ResourceStableID uuid.UUID             `json:"resource_stable_id"`
	Status           domain.ProgressStatus `json:"status"`
}

// GetCourseProgress lista el avance por recurso de un estudiante inscrito.
func (uc *UseCase) GetCourseProgress(ctx context.Context, studentID, courseID uuid.UUID) ([]ResourceProgressItem, error) {
	if _, err := uc.repo.GetEnrollment(ctx, studentID, courseID); err != nil {
		return nil, domain.ErrNotEnrolled
	}
	list, err := uc.repo.GetStudentCourseProgress(ctx, studentID, courseID)
	if err != nil {
		return nil, err
	}
	out := make([]ResourceProgressItem, 0, len(list))
	for _, p := range list {
		out = append(out, ResourceProgressItem{ResourceStableID: p.ResourceStableID, Status: p.Status})
	}
	return out, nil
}

func (uc *UseCase) RecordHeartbeat(ctx context.Context, input HeartbeatInput) (*domain.Enrollment, error) {
	if input.DwellTimeSeconds < 0 || input.LastPositionSeconds < 0 {
		return nil, errors.New("los valores de tiempo de permanencia y posición deben ser no negativos")
	}

	enrollment, err := uc.repo.GetEnrollment(ctx, input.StudentID, input.CourseID)
	if err != nil {
		return nil, domain.ErrNotEnrolled
	}

	// Limitar permanencia reportada por pulso individual a máximo 300 segundos por seguridad anti-manipulación
	dwellTime := input.DwellTimeSeconds
	if dwellTime > 300 {
		dwellTime = 300
	}

	status := domain.ProgressOpened
	if dwellTime >= 10 {
		status = domain.ProgressCompleted
	}

	prog := &domain.ResourceProgress{
		ID:                  uuid.New(),
		StudentID:           input.StudentID,
		CourseID:            input.CourseID,
		ResourceStableID:    input.ResourceStableID,
		Status:              status,
		DwellTimeSeconds:    dwellTime,
		LastPositionSeconds: input.LastPositionSeconds,
	}

	if err := uc.repo.UpsertResourceProgress(ctx, prog); err != nil {
		return nil, err
	}

	course, err := uc.courseRepo.GetCourseByID(ctx, input.CourseID)
	if err != nil || course.CurrentPublishedVersionID == nil {
		return enrollment, nil
	}

	version, err := uc.courseRepo.GetFullVersionHierarchy(ctx, *course.CurrentPublishedVersionID)
	if err != nil {
		return enrollment, nil
	}

	mandatoryResourceStableIDs := make(map[uuid.UUID]bool)
	for _, mod := range version.Modules {
		for _, unit := range mod.Units {
			for _, res := range unit.Resources {
				if res.IsVisible && res.IsMandatory {
					mandatoryResourceStableIDs[res.StableID] = true
				}
			}
		}
	}

	totalMandatory := len(mandatoryResourceStableIDs)
	if totalMandatory == 0 {
		return enrollment, nil
	}

	studentProgress, err := uc.repo.GetStudentCourseProgress(ctx, input.StudentID, input.CourseID)
	if err != nil {
		return nil, err
	}

	completedCount := 0
	for _, p := range studentProgress {
		if p.Status == domain.ProgressCompleted && mandatoryResourceStableIDs[p.ResourceStableID] {
			completedCount++
		}
	}

	newPercentage := (float64(completedCount) / float64(totalMandatory)) * 100.0
	enrollment.ProgressPercentage = newPercentage

	if newPercentage >= 100.0 {
		enrollment.AcademicStatus = domain.AcademicApproved
		_, _ = uc.IssueBadge(ctx, input.StudentID, input.CourseID, version.ID)
	}

	if err := uc.repo.UpdateEnrollment(ctx, enrollment); err != nil {
		return nil, err
	}

	return enrollment, nil
}

func (uc *UseCase) IssueBadge(ctx context.Context, studentID, courseID, versionID uuid.UUID) (*domain.Badge, error) {
	existing, err := uc.repo.GetStudentBadgeForCourse(ctx, studentID, courseID)
	if err == nil && existing != nil {
		return existing, nil
	}

	verificationCode := uuid.New()
	verificationURL := fmt.Sprintf("%s/api/v1/badges/verify/%s", uc.baseURL, verificationCode.String())
	imageURL := fmt.Sprintf("%s/media/badges/%s.png", uc.baseURL, verificationCode.String())

	badge := &domain.Badge{
		ID:               uuid.New(),
		StudentID:        studentID,
		CourseID:         courseID,
		CourseVersionID:  versionID,
		VerificationCode: verificationCode,
		ImageURL:         imageURL,
		VerificationURL:  verificationURL,
		IsRevoked:        false,
		IssuedAt:         time.Now(),
	}

	if err := uc.repo.CreateBadge(ctx, badge); err != nil {
		return nil, err
	}

	return badge, nil
}

func (uc *UseCase) RevokeBadge(ctx context.Context, adminID, badgeID uuid.UUID) error {
	badge, err := uc.repo.GetBadgeByID(ctx, badgeID)
	if err != nil {
		return err
	}
	if badge.IsRevoked {
		return nil
	}
	if err := uc.repo.RevokeBadge(ctx, badgeID); err != nil {
		return err
	}

	_ = uc.userRepo.CreateAuditLog(ctx, &domain.AuditLog{
		ActorID:        &adminID,
		Action:         "BADGE_REVOKED",
		TargetResource: "badges",
		TargetID:       &badgeID,
		Payload:        map[string]any{"student_id": badge.StudentID, "course_id": badge.CourseID},
	})
	return nil
}

func (uc *UseCase) VerifyBadge(ctx context.Context, verificationCode uuid.UUID) (*domain.Badge, error) {
	return uc.repo.GetBadgeByCode(ctx, verificationCode)
}

// EnrollmentSummary une la inscripción con los datos del curso para listados.
type EnrollmentSummary struct {
	domain.Enrollment
	CourseTitle   string `json:"course_title"`
	CourseSlug    string `json:"course_slug"`
	CourseSummary string `json:"course_summary"`
}

// enrollmentLister es una capacidad opcional del repositorio: se consulta por
// type assertion para no ampliar domain.LearningRepository (y sus mocks).
type enrollmentLister interface {
	ListEnrollmentsByStudent(ctx context.Context, studentID uuid.UUID) ([]EnrollmentSummary, error)
}

func (uc *UseCase) ListStudentEnrollments(ctx context.Context, studentID uuid.UUID) ([]EnrollmentSummary, error) {
	lister, ok := uc.repo.(enrollmentLister)
	if !ok {
		return nil, errors.New("el repositorio no soporta listar inscripciones")
	}
	items, err := lister.ListEnrollmentsByStudent(ctx, studentID)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []EnrollmentSummary{}
	}
	return items, nil
}

// GetQuizSnapshotByResource localiza el quiz de un recurso y devuelve su
// snapshot seguro.
func (uc *UseCase) GetQuizSnapshotByResource(ctx context.Context, resourceID uuid.UUID) (*QuizSnapshotResponse, error) {
	quiz, err := uc.repo.GetQuizByResourceID(ctx, resourceID)
	if err != nil {
		return nil, err
	}
	return uc.GetQuizSnapshot(ctx, quiz.ID)
}

// AuthorizeQuizEdit comprueba que el usuario puede editar el curso al que
// pertenece el recurso donde se define el quiz.
func (uc *UseCase) AuthorizeQuizEdit(ctx context.Context, user *domain.User, resourceID uuid.UUID) error {
	return domain.AuthorizeCourseEdit(ctx, uc.courseRepo, user, domain.EntityResource, resourceID)
}
