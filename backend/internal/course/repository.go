package course

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

func (r *PostgresRepository) CreateCourse(ctx context.Context, c *domain.Course, v *domain.CourseVersion) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	c.CreatedAt = now
	c.UpdatedAt = now

	courseQuery := `
		INSERT INTO courses (id, slug, title, summary, created_by_teacher_id, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = tx.ExecContext(ctx, courseQuery, c.ID, c.Slug, c.Title, c.Summary, c.CreatedByTeacherID, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create course: %w", err)
	}

	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	v.CourseID = c.ID
	v.VersionNumber = 1
	v.Status = domain.VersionStatusDraft
	v.CreatedAt = now

	versionQuery := `
		INSERT INTO course_versions (id, course_id, version_number, status, passing_score, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err = tx.ExecContext(ctx, versionQuery, v.ID, v.CourseID, v.VersionNumber, string(v.Status), v.PassingScore, v.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create initial version: %w", err)
	}

	return tx.Commit()
}

func (r *PostgresRepository) GetCourseByID(ctx context.Context, id uuid.UUID) (*domain.Course, error) {
	query := `
		SELECT id, slug, title, summary, created_by_teacher_id, current_published_version_id, created_at, updated_at
		FROM courses WHERE id = $1
	`
	var c domain.Course
	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&c.ID, &c.Slug, &c.Title, &c.Summary, &c.CreatedByTeacherID, &c.CurrentPublishedVersionID, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrCourseNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *PostgresRepository) GetCourseBySlug(ctx context.Context, slug string) (*domain.Course, error) {
	query := `
		SELECT id, slug, title, summary, created_by_teacher_id, current_published_version_id, created_at, updated_at
		FROM courses WHERE slug = $1
	`
	var c domain.Course
	err := r.db.QueryRowContext(ctx, query, slug).Scan(
		&c.ID, &c.Slug, &c.Title, &c.Summary, &c.CreatedByTeacherID, &c.CurrentPublishedVersionID, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrCourseNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *PostgresRepository) ListCourses(ctx context.Context, limit, offset int) ([]*domain.Course, error) {
	query := `
		SELECT id, slug, title, summary, created_by_teacher_id, current_published_version_id, created_at, updated_at
		FROM courses ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	courses := make([]*domain.Course, 0)
	for rows.Next() {
		var c domain.Course
		if err := rows.Scan(&c.ID, &c.Slug, &c.Title, &c.Summary, &c.CreatedByTeacherID, &c.CurrentPublishedVersionID, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, err
		}
		courses = append(courses, &c)
	}
	return courses, nil
}

func (r *PostgresRepository) UpdateCourse(ctx context.Context, c *domain.Course) error {
	query := `
		UPDATE courses
		SET slug = $1, title = $2, summary = $3, current_published_version_id = $4, updated_at = $5
		WHERE id = $6
	`
	c.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query, c.Slug, c.Title, c.Summary, c.CurrentPublishedVersionID, c.UpdatedAt, c.ID)
	return err
}

func (r *PostgresRepository) CreateVersion(ctx context.Context, v *domain.CourseVersion) error {
	query := `
		INSERT INTO course_versions (id, course_id, version_number, status, passing_score, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	v.CreatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query, v.ID, v.CourseID, v.VersionNumber, string(v.Status), v.PassingScore, v.CreatedAt)
	return err
}

func (r *PostgresRepository) GetVersionByID(ctx context.Context, versionID uuid.UUID) (*domain.CourseVersion, error) {
	query := `
		SELECT id, course_id, version_number, status, passing_score, created_at, published_at
		FROM course_versions WHERE id = $1
	`
	var v domain.CourseVersion
	var statusStr string
	err := r.db.QueryRowContext(ctx, query, versionID).Scan(
		&v.ID, &v.CourseID, &v.VersionNumber, &statusStr, &v.PassingScore, &v.CreatedAt, &v.PublishedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrVersionNotFound
		}
		return nil, err
	}
	v.Status = domain.VersionStatus(statusStr)
	return &v, nil
}

func (r *PostgresRepository) GetLatestDraftVersion(ctx context.Context, courseID uuid.UUID) (*domain.CourseVersion, error) {
	query := `
		SELECT id, course_id, version_number, status, passing_score, created_at, published_at
		FROM course_versions WHERE course_id = $1 AND status = 'draft'
		ORDER BY version_number DESC LIMIT 1
	`
	var v domain.CourseVersion
	var statusStr string
	err := r.db.QueryRowContext(ctx, query, courseID).Scan(
		&v.ID, &v.CourseID, &v.VersionNumber, &statusStr, &v.PassingScore, &v.CreatedAt, &v.PublishedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrVersionNotFound
		}
		return nil, err
	}
	v.Status = domain.VersionStatus(statusStr)
	return &v, nil
}

func (r *PostgresRepository) GetFullVersionHierarchy(ctx context.Context, versionID uuid.UUID) (*domain.CourseVersion, error) {
	v, err := r.GetVersionByID(ctx, versionID)
	if err != nil {
		return nil, err
	}

	modulesQuery := `
		SELECT id, version_id, stable_id, title, description, position, created_at
		FROM course_modules WHERE version_id = $1 ORDER BY position ASC
	`
	mRows, err := r.db.QueryContext(ctx, modulesQuery, versionID)
	if err != nil {
		return nil, err
	}
	defer mRows.Close()

	modules := make([]domain.Module, 0)
	for mRows.Next() {
		var m domain.Module
		if err := mRows.Scan(&m.ID, &m.VersionID, &m.StableID, &m.Title, &m.Description, &m.Position, &m.CreatedAt); err != nil {
			return nil, err
		}

		unitsQuery := `
			SELECT id, module_id, stable_id, title, position, created_at
			FROM course_units WHERE module_id = $1 ORDER BY position ASC
		`
		uRows, err := r.db.QueryContext(ctx, unitsQuery, m.ID)
		if err != nil {
			return nil, err
		}

		units := make([]domain.Unit, 0)
		for uRows.Next() {
			var u domain.Unit
			if err := uRows.Scan(&u.ID, &u.ModuleID, &u.StableID, &u.Title, &u.Position, &u.CreatedAt); err != nil {
				uRows.Close()
				return nil, err
			}

			resourcesQuery := `
				SELECT id, unit_id, stable_id, title, type, canonical_markdown, media_url, is_visible, is_mandatory, is_downloadable, position, processing_status, created_at, updated_at, last_autosaved_at
				FROM course_resources WHERE unit_id = $1 ORDER BY position ASC
			`
			rRows, err := r.db.QueryContext(ctx, resourcesQuery, u.ID)
			if err != nil {
				uRows.Close()
				return nil, err
			}

			resources := make([]domain.Resource, 0)
			for rRows.Next() {
				var res domain.Resource
				var typeStr, procStr string
				var canonicalMarkdown, mediaURL sql.NullString
				var lastAutosavedAt sql.NullTime
				if err := rRows.Scan(
					&res.ID, &res.UnitID, &res.StableID, &res.Title, &typeStr, &canonicalMarkdown, &mediaURL,
					&res.IsVisible, &res.IsMandatory, &res.IsDownloadable, &res.Position, &procStr, &res.CreatedAt, &res.UpdatedAt, &lastAutosavedAt,
				); err != nil {
					rRows.Close()
					uRows.Close()
					return nil, err
				}
				res.Type = domain.ResourceType(typeStr)
				res.ProcessingStatus = domain.ProcessingStatus(procStr)
				res.CanonicalMarkdown = canonicalMarkdown.String
				res.MediaURL = mediaURL.String
				if lastAutosavedAt.Valid {
					res.LastAutosavedAt = &lastAutosavedAt.Time
				}
				resources = append(resources, res)
			}
			rRows.Close()
			u.Resources = resources
			units = append(units, u)
		}
		uRows.Close()
		m.Units = units
		modules = append(modules, m)
	}

	v.Modules = modules
	return v, nil
}

func (r *PostgresRepository) PublishVersion(ctx context.Context, courseID uuid.UUID, versionID uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now()
	updateVersionQuery := `UPDATE course_versions SET status = 'published', published_at = $1 WHERE id = $2`
	if _, err := tx.ExecContext(ctx, updateVersionQuery, now, versionID); err != nil {
		return err
	}

	updateCourseQuery := `UPDATE courses SET current_published_version_id = $1, updated_at = $2 WHERE id = $3`
	if _, err := tx.ExecContext(ctx, updateCourseQuery, versionID, now, courseID); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *PostgresRepository) UnpublishCourse(ctx context.Context, courseID uuid.UUID) error {
	query := `UPDATE courses SET current_published_version_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, courseID)
	return err
}

func (r *PostgresRepository) CreateModule(ctx context.Context, m *domain.Module) error {
	query := `
		INSERT INTO course_modules (id, version_id, stable_id, title, description, position, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	if m.StableID == uuid.Nil {
		m.StableID = uuid.New()
	}
	m.CreatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query, m.ID, m.VersionID, m.StableID, m.Title, m.Description, m.Position, m.CreatedAt)
	return err
}

func (r *PostgresRepository) CreateUnit(ctx context.Context, u *domain.Unit) error {
	query := `
		INSERT INTO course_units (id, module_id, stable_id, title, position, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	if u.StableID == uuid.Nil {
		u.StableID = uuid.New()
	}
	u.CreatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query, u.ID, u.ModuleID, u.StableID, u.Title, u.Position, u.CreatedAt)
	return err
}

func (r *PostgresRepository) CreateResource(ctx context.Context, res *domain.Resource) error {
	query := `
		INSERT INTO course_resources (id, unit_id, stable_id, title, type, canonical_markdown, media_url, is_visible, is_mandatory, is_downloadable, position, processing_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`
	if res.ID == uuid.Nil {
		res.ID = uuid.New()
	}
	if res.StableID == uuid.Nil {
		res.StableID = uuid.New()
	}
	now := time.Now()
	res.CreatedAt = now
	res.UpdatedAt = now
	if res.ProcessingStatus == "" {
		res.ProcessingStatus = domain.ProcessingCompleted
	}
	_, err := r.db.ExecContext(ctx, query,
		res.ID, res.UnitID, res.StableID, res.Title, string(res.Type), res.CanonicalMarkdown, res.MediaURL,
		res.IsVisible, res.IsMandatory, res.IsDownloadable, res.Position, string(res.ProcessingStatus), res.CreatedAt, res.UpdatedAt,
	)
	return err
}

func (r *PostgresRepository) GetResourceByID(ctx context.Context, resourceID uuid.UUID) (*domain.Resource, error) {
	query := `
		SELECT id, unit_id, stable_id, title, type, canonical_markdown, media_url, is_visible, is_mandatory, is_downloadable, position, processing_status, created_at, updated_at, last_autosaved_at
		FROM course_resources WHERE id = $1
	`
	var res domain.Resource
	var typeStr, procStr string
	var canonicalMarkdown, mediaURL sql.NullString
	var lastAutosavedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, query, resourceID).Scan(
		&res.ID, &res.UnitID, &res.StableID, &res.Title, &typeStr, &canonicalMarkdown, &mediaURL,
		&res.IsVisible, &res.IsMandatory, &res.IsDownloadable, &res.Position, &procStr, &res.CreatedAt, &res.UpdatedAt, &lastAutosavedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrResourceNotFound
		}
		return nil, err
	}
	res.Type = domain.ResourceType(typeStr)
	res.ProcessingStatus = domain.ProcessingStatus(procStr)
	res.CanonicalMarkdown = canonicalMarkdown.String
	res.MediaURL = mediaURL.String
	if lastAutosavedAt.Valid {
		res.LastAutosavedAt = &lastAutosavedAt.Time
	}
	return &res, nil
}

func (r *PostgresRepository) UpdateResource(ctx context.Context, res *domain.Resource) error {
	query := `
		UPDATE course_resources
		SET title = $1, canonical_markdown = $2, media_url = $3, is_visible = $4, is_mandatory = $5, is_downloadable = $6, position = $7, processing_status = $8, updated_at = $9
		WHERE id = $10
	`
	res.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query,
		res.Title, res.CanonicalMarkdown, res.MediaURL, res.IsVisible, res.IsMandatory, res.IsDownloadable, res.Position, string(res.ProcessingStatus), res.UpdatedAt, res.ID,
	)
	return err
}

// AutosaveResource actualiza SOLO title/canonical_markdown (si vienen) y
// last_autosaved_at. A diferencia de UpdateResource, nunca toca
// is_visible/is_mandatory/is_downloadable/position/processing_status —
// evita exactamente el bug que hacia inestable el autosave: un guardado
// parcial no debe poder despublicar un recurso por efecto secundario.
func (r *PostgresRepository) AutosaveResource(ctx context.Context, resourceID uuid.UUID, title *string, markdown *string, at time.Time) error {
	query := `
		UPDATE course_resources
		SET
			title = COALESCE($1, title),
			canonical_markdown = COALESCE($2, canonical_markdown),
			last_autosaved_at = $3,
			updated_at = $3
		WHERE id = $4
	`
	var titleArg, markdownArg sql.NullString
	if title != nil {
		titleArg = sql.NullString{String: *title, Valid: true}
	}
	if markdown != nil {
		markdownArg = sql.NullString{String: *markdown, Valid: true}
	}
	result, err := r.db.ExecContext(ctx, query, titleArg, markdownArg, at, resourceID)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return domain.ErrResourceNotFound
	}
	return nil
}

func (r *PostgresRepository) DeleteResource(ctx context.Context, resourceID uuid.UUID) error {
	query := `DELETE FROM course_resources WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, resourceID)
	return err
}

func (r *PostgresRepository) ReorderModules(ctx context.Context, versionID uuid.UUID, orderedIDs []uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `UPDATE course_modules SET position = $1 WHERE id = $2 AND version_id = $3`
	for pos, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, query, pos+1, id, versionID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) ReorderUnits(ctx context.Context, moduleID uuid.UUID, orderedIDs []uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `UPDATE course_units SET position = $1 WHERE id = $2 AND module_id = $3`
	for pos, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, query, pos+1, id, moduleID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *PostgresRepository) ReorderResources(ctx context.Context, unitID uuid.UUID, orderedIDs []uuid.UUID) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	query := `UPDATE course_resources SET position = $1 WHERE id = $2 AND unit_id = $3`
	for pos, id := range orderedIDs {
		if _, err := tx.ExecContext(ctx, query, pos+1, id, unitID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
