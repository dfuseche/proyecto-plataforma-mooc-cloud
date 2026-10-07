package learning

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/mooc-platform/backend/internal/domain"
)

type PostgresRepository struct {
	db *sql.DB
}

func NewPostgresRepository(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) EnrollStudent(ctx context.Context, e *domain.Enrollment) error {
	query := `
		INSERT INTO enrollments (id, student_id, course_id, status, academic_status, progress_percentage, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (student_id, course_id) DO UPDATE
		SET status = 'active', updated_at = CURRENT_TIMESTAMP
	`
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	now := time.Now()
	e.CreatedAt = now
	e.UpdatedAt = now
	_, err := r.db.ExecContext(ctx, query, e.ID, e.StudentID, e.CourseID, string(e.Status), string(e.AcademicStatus), e.ProgressPercentage, e.CreatedAt, e.UpdatedAt)
	return err
}

func (r *PostgresRepository) GetEnrollment(ctx context.Context, studentID, courseID uuid.UUID) (*domain.Enrollment, error) {
	query := `
		SELECT id, student_id, course_id, status, academic_status, progress_percentage, created_at, updated_at
		FROM enrollments WHERE student_id = $1 AND course_id = $2
	`
	var e domain.Enrollment
	var statusStr, acadStr string
	err := r.db.QueryRowContext(ctx, query, studentID, courseID).Scan(
		&e.ID, &e.StudentID, &e.CourseID, &statusStr, &acadStr, &e.ProgressPercentage, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrNotEnrolled
		}
		return nil, err
	}
	e.Status = domain.EnrollmentStatus(statusStr)
	e.AcademicStatus = domain.AcademicStatus(acadStr)
	return &e, nil
}

func (r *PostgresRepository) UpdateEnrollment(ctx context.Context, e *domain.Enrollment) error {
	query := `
		UPDATE enrollments
		SET status = $1, academic_status = $2, progress_percentage = $3, updated_at = $4
		WHERE id = $5
	`
	e.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query, string(e.Status), string(e.AcademicStatus), e.ProgressPercentage, e.UpdatedAt, e.ID)
	return err
}

func (r *PostgresRepository) CreateQuiz(ctx context.Context, q *domain.Quiz) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if q.ID == uuid.Nil {
		q.ID = uuid.New()
	}
	q.CreatedAt = time.Now()

	quizQuery := `
		INSERT INTO quizzes (id, resource_id, max_attempts, passing_score, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`
	if _, err := tx.ExecContext(ctx, quizQuery, q.ID, q.ResourceID, q.MaxAttempts, q.PassingScore, q.CreatedAt); err != nil {
		return err
	}

	for i := range q.Questions {
		if q.Questions[i].ID == uuid.Nil {
			q.Questions[i].ID = uuid.New()
		}
		q.Questions[i].QuizID = q.ID
		questQuery := `
			INSERT INTO quiz_questions (id, quiz_id, question_text, position, points, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`
		if _, err := tx.ExecContext(ctx, questQuery, q.Questions[i].ID, q.Questions[i].QuizID, q.Questions[i].QuestionText, q.Questions[i].Position, q.Questions[i].Points, time.Now()); err != nil {
			return err
		}

		for j := range q.Questions[i].Options {
			if q.Questions[i].Options[j].ID == uuid.Nil {
				q.Questions[i].Options[j].ID = uuid.New()
			}
			q.Questions[i].Options[j].QuestionID = q.Questions[i].ID
			optQuery := `
				INSERT INTO quiz_options (id, question_id, option_text, is_correct, feedback, position)
				VALUES ($1, $2, $3, $4, $5, $6)
			`
			opt := q.Questions[i].Options[j]
			if _, err := tx.ExecContext(ctx, optQuery, opt.ID, opt.QuestionID, opt.OptionText, opt.IsCorrect, opt.Feedback, opt.Position); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (r *PostgresRepository) GetQuizByResourceID(ctx context.Context, resourceID uuid.UUID) (*domain.Quiz, error) {
	query := `SELECT id, resource_id, max_attempts, passing_score, created_at FROM quizzes WHERE resource_id = $1`
	var q domain.Quiz
	err := r.db.QueryRowContext(ctx, query, resourceID).Scan(&q.ID, &q.ResourceID, &q.MaxAttempts, &q.PassingScore, &q.CreatedAt)
	if err != nil {
		return nil, err
	}
	return r.GetQuizWithAnswers(ctx, q.ID)
}

func (r *PostgresRepository) GetQuizWithAnswers(ctx context.Context, quizID uuid.UUID) (*domain.Quiz, error) {
	query := `SELECT id, resource_id, max_attempts, passing_score, created_at FROM quizzes WHERE id = $1`
	var q domain.Quiz
	err := r.db.QueryRowContext(ctx, query, quizID).Scan(&q.ID, &q.ResourceID, &q.MaxAttempts, &q.PassingScore, &q.CreatedAt)
	if err != nil {
		return nil, err
	}

	questQuery := `SELECT id, quiz_id, question_text, position, points, created_at FROM quiz_questions WHERE quiz_id = $1 ORDER BY position ASC`
	qRows, err := r.db.QueryContext(ctx, questQuery, quizID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = qRows.Close() }()

	questions := make([]domain.QuizQuestion, 0)
	for qRows.Next() {
		var quest domain.QuizQuestion
		if err := qRows.Scan(&quest.ID, &quest.QuizID, &quest.QuestionText, &quest.Position, &quest.Points, &quest.CreatedAt); err != nil {
			return nil, err
		}

		optQuery := `SELECT id, question_id, option_text, is_correct, feedback, position FROM quiz_options WHERE question_id = $1 ORDER BY position ASC`
		oRows, err := r.db.QueryContext(ctx, optQuery, quest.ID)
		if err != nil {
			return nil, err
		}

		options := make([]domain.QuizOption, 0)
		for oRows.Next() {
			var opt domain.QuizOption
			var feedback sql.NullString
			if err := oRows.Scan(&opt.ID, &opt.QuestionID, &opt.OptionText, &opt.IsCorrect, &feedback, &opt.Position); err != nil {
				_ = oRows.Close()
				return nil, err
			}
			opt.Feedback = feedback.String
			options = append(options, opt)
		}
		_ = oRows.Close()
		quest.Options = options
		questions = append(questions, quest)
	}

	q.Questions = questions
	return &q, nil
}

func (r *PostgresRepository) CreateQuizAttempt(ctx context.Context, a *domain.QuizAttempt) error {
	query := `
		INSERT INTO quiz_attempts (id, student_id, quiz_id, attempt_number, status, score, answers, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	a.CreatedAt = time.Now()
	answersJSON, _ := json.Marshal(a.Answers)
	_, err := r.db.ExecContext(ctx, query, a.ID, a.StudentID, a.QuizID, a.AttemptNumber, string(a.Status), a.Score, answersJSON, a.CreatedAt)
	return err
}

func (r *PostgresRepository) GetAttemptByID(ctx context.Context, attemptID uuid.UUID) (*domain.QuizAttempt, error) {
	query := `
		SELECT id, student_id, quiz_id, attempt_number, status, score, answers, submitted_at, created_at
		FROM quiz_attempts WHERE id = $1
	`
	var a domain.QuizAttempt
	var statusStr string
	var answersBytes []byte
	err := r.db.QueryRowContext(ctx, query, attemptID).Scan(
		&a.ID, &a.StudentID, &a.QuizID, &a.AttemptNumber, &statusStr, &a.Score, &answersBytes, &a.SubmittedAt, &a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.Status = domain.AttemptStatus(statusStr)
	_ = json.Unmarshal(answersBytes, &a.Answers)
	return &a, nil
}

func (r *PostgresRepository) GetStudentAttemptsCount(ctx context.Context, studentID, quizID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM quiz_attempts WHERE student_id = $1 AND quiz_id = $2`
	var count int
	err := r.db.QueryRowContext(ctx, query, studentID, quizID).Scan(&count)
	return count, err
}

func (r *PostgresRepository) GetStudentSubmittedAttemptsCount(ctx context.Context, studentID, quizID uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM quiz_attempts WHERE student_id = $1 AND quiz_id = $2 AND status = 'submitted'`
	var count int
	err := r.db.QueryRowContext(ctx, query, studentID, quizID).Scan(&count)
	return count, err
}

func (r *PostgresRepository) GetActiveAttempt(ctx context.Context, studentID, quizID uuid.UUID) (*domain.QuizAttempt, error) {
	query := `
		SELECT id, student_id, quiz_id, attempt_number, status, score, answers, submitted_at, created_at
		FROM quiz_attempts
		WHERE student_id = $1 AND quiz_id = $2 AND status = 'in_progress'
		ORDER BY created_at DESC LIMIT 1
	`
	var a domain.QuizAttempt
	var statusStr string
	var answersBytes []byte
	err := r.db.QueryRowContext(ctx, query, studentID, quizID).Scan(
		&a.ID, &a.StudentID, &a.QuizID, &a.AttemptNumber, &statusStr, &a.Score, &answersBytes, &a.SubmittedAt, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	a.Status = domain.AttemptStatus(statusStr)
	if len(answersBytes) > 0 {
		_ = json.Unmarshal(answersBytes, &a.Answers)
	}
	if a.Answers == nil {
		a.Answers = make(map[string]string)
	}
	return &a, nil
}

func (r *PostgresRepository) UpdateQuizAttempt(ctx context.Context, a *domain.QuizAttempt) error {
	query := `
		UPDATE quiz_attempts
		SET status = $1, score = $2, answers = $3, submitted_at = $4
		WHERE id = $5
	`
	answersJSON, _ := json.Marshal(a.Answers)
	_, err := r.db.ExecContext(ctx, query, string(a.Status), a.Score, answersJSON, a.SubmittedAt, a.ID)
	return err
}

func (r *PostgresRepository) UpsertResourceProgress(ctx context.Context, p *domain.ResourceProgress) error {
	query := `
		INSERT INTO resource_progress (id, student_id, course_id, resource_stable_id, status, dwell_time_seconds, last_position_seconds, last_heartbeat_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (student_id, course_id, resource_stable_id) DO UPDATE
		SET status = EXCLUDED.status,
		    dwell_time_seconds = resource_progress.dwell_time_seconds + EXCLUDED.dwell_time_seconds,
		    last_position_seconds = EXCLUDED.last_position_seconds,
		    last_heartbeat_at = EXCLUDED.last_heartbeat_at,
		    updated_at = EXCLUDED.updated_at
	`
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	now := time.Now()
	p.CreatedAt = now
	p.UpdatedAt = now
	p.LastHeartbeatAt = now
	_, err := r.db.ExecContext(ctx, query,
		p.ID, p.StudentID, p.CourseID, p.ResourceStableID, string(p.Status), p.DwellTimeSeconds, p.LastPositionSeconds, p.LastHeartbeatAt, p.CreatedAt, p.UpdatedAt,
	)
	return err
}

func (r *PostgresRepository) GetStudentCourseProgress(ctx context.Context, studentID, courseID uuid.UUID) ([]*domain.ResourceProgress, error) {
	query := `
		SELECT id, student_id, course_id, resource_stable_id, status, dwell_time_seconds, last_position_seconds, last_heartbeat_at, created_at, updated_at
		FROM resource_progress WHERE student_id = $1 AND course_id = $2
	`
	rows, err := r.db.QueryContext(ctx, query, studentID, courseID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	list := make([]*domain.ResourceProgress, 0)
	for rows.Next() {
		var p domain.ResourceProgress
		var statusStr string
		if err := rows.Scan(&p.ID, &p.StudentID, &p.CourseID, &p.ResourceStableID, &statusStr, &p.DwellTimeSeconds, &p.LastPositionSeconds, &p.LastHeartbeatAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Status = domain.ProgressStatus(statusStr)
		list = append(list, &p)
	}
	return list, nil
}

func (r *PostgresRepository) CreateBadge(ctx context.Context, b *domain.Badge) error {
	query := `
		INSERT INTO badges (id, student_id, course_id, course_version_id, verification_code, image_url, verification_url, is_revoked, issued_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.VerificationCode == uuid.Nil {
		b.VerificationCode = uuid.New()
	}
	b.IssuedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query, b.ID, b.StudentID, b.CourseID, b.CourseVersionID, b.VerificationCode, b.ImageURL, b.VerificationURL, b.IsRevoked, b.IssuedAt)
	return err
}

func (r *PostgresRepository) GetBadgeByCode(ctx context.Context, code uuid.UUID) (*domain.Badge, error) {
	query := `
		SELECT id, student_id, course_id, course_version_id, verification_code, image_url, verification_url, is_revoked, issued_at
		FROM badges WHERE verification_code = $1
	`
	var b domain.Badge
	err := r.db.QueryRowContext(ctx, query, code).Scan(&b.ID, &b.StudentID, &b.CourseID, &b.CourseVersionID, &b.VerificationCode, &b.ImageURL, &b.VerificationURL, &b.IsRevoked, &b.IssuedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *PostgresRepository) GetStudentBadgeForCourse(ctx context.Context, studentID, courseID uuid.UUID) (*domain.Badge, error) {
	query := `
		SELECT id, student_id, course_id, course_version_id, verification_code, image_url, verification_url, is_revoked, issued_at
		FROM badges WHERE student_id = $1 AND course_id = $2
	`
	var b domain.Badge
	err := r.db.QueryRowContext(ctx, query, studentID, courseID).Scan(&b.ID, &b.StudentID, &b.CourseID, &b.CourseVersionID, &b.VerificationCode, &b.ImageURL, &b.VerificationURL, &b.IsRevoked, &b.IssuedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *PostgresRepository) GetBadgeByID(ctx context.Context, badgeID uuid.UUID) (*domain.Badge, error) {
	query := `
		SELECT id, student_id, course_id, course_version_id, verification_code, image_url, verification_url, is_revoked, issued_at
		FROM badges WHERE id = $1
	`
	var b domain.Badge
	err := r.db.QueryRowContext(ctx, query, badgeID).Scan(&b.ID, &b.StudentID, &b.CourseID, &b.CourseVersionID, &b.VerificationCode, &b.ImageURL, &b.VerificationURL, &b.IsRevoked, &b.IssuedAt)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *PostgresRepository) RevokeBadge(ctx context.Context, badgeID uuid.UUID) error {
	query := `UPDATE badges SET is_revoked = TRUE WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, badgeID)
	return err
}
