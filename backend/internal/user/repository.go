package user

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

func (r *PostgresRepository) CreateUser(ctx context.Context, u *domain.User) error {
	query := `
		INSERT INTO users (id, email, password_hash, full_name, role, status, email_verified_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	now := time.Now()
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	u.CreatedAt = now
	u.UpdatedAt = now

	_, err := r.db.ExecContext(ctx, query,
		u.ID, u.Email, u.PasswordHash, u.FullName, string(u.Role), string(u.Status), u.EmailVerifiedAt, u.CreatedAt, u.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	query := `
		SELECT id, email, password_hash, full_name, role, status, email_verified_at, created_at, updated_at
		FROM users WHERE id = $1
	`
	row := r.db.QueryRowContext(ctx, query, id)
	var u domain.User
	var roleStr, statusStr string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &roleStr, &statusStr, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	u.Role = domain.Role(roleStr)
	u.Status = domain.UserStatus(statusStr)
	return &u, nil
}

func (r *PostgresRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	query := `
		SELECT id, email, password_hash, full_name, role, status, email_verified_at, created_at, updated_at
		FROM users WHERE LOWER(email) = LOWER($1)
	`
	row := r.db.QueryRowContext(ctx, query, email)
	var u domain.User
	var roleStr, statusStr string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &roleStr, &statusStr, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrUserNotFound
		}
		return nil, err
	}
	u.Role = domain.Role(roleStr)
	u.Status = domain.UserStatus(statusStr)
	return &u, nil
}

func (r *PostgresRepository) UpdateUser(ctx context.Context, u *domain.User) error {
	query := `
		UPDATE users
		SET email = $1, password_hash = $2, full_name = $3, role = $4, status = $5, email_verified_at = $6, updated_at = $7
		WHERE id = $8
	`
	u.UpdatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query,
		u.Email, u.PasswordHash, u.FullName, string(u.Role), string(u.Status), u.EmailVerifiedAt, u.UpdatedAt, u.ID,
	)
	return err
}

func (r *PostgresRepository) CountActiveAdmins(ctx context.Context) (int, error) {
	query := `SELECT COUNT(*) FROM users WHERE role = 'admin' AND status = 'active'`
	var count int
	err := r.db.QueryRowContext(ctx, query).Scan(&count)
	return count, err
}

func (r *PostgresRepository) CreateSession(ctx context.Context, s *domain.UserSession) error {
	query := `
		INSERT INTO user_sessions (id, user_id, token, user_agent, ip_address, is_revoked, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	now := time.Now()
	s.CreatedAt = now
	_, err := r.db.ExecContext(ctx, query,
		s.ID, s.UserID, s.Token, s.UserAgent, s.IPAddress, s.IsRevoked, s.ExpiresAt, s.CreatedAt, now,
	)
	return err
}

func (r *PostgresRepository) GetSessionByToken(ctx context.Context, token string) (*domain.UserSession, error) {
	query := `
		SELECT id, user_id, token, user_agent, ip_address, is_revoked, expires_at, created_at
		FROM user_sessions WHERE token = $1
	`
	var s domain.UserSession
	err := r.db.QueryRowContext(ctx, query, token).Scan(
		&s.ID, &s.UserID, &s.Token, &s.UserAgent, &s.IPAddress, &s.IsRevoked, &s.ExpiresAt, &s.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrSessionExpired
		}
		return nil, err
	}
	return &s, nil
}

func (r *PostgresRepository) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	query := `UPDATE user_sessions SET is_revoked = TRUE, updated_at = CURRENT_TIMESTAMP WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, sessionID)
	return err
}

func (r *PostgresRepository) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	query := `UPDATE user_sessions SET is_revoked = TRUE, updated_at = CURRENT_TIMESTAMP WHERE user_id = $1`
	_, err := r.db.ExecContext(ctx, query, userID)
	return err
}

func (r *PostgresRepository) CreateToken(ctx context.Context, t *domain.UserToken) error {
	query := `
		INSERT INTO user_tokens (id, user_id, token, type, used, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	t.CreatedAt = time.Now()
	_, err := r.db.ExecContext(ctx, query,
		t.ID, t.UserID, t.Token, t.Type, t.Used, t.ExpiresAt, t.CreatedAt,
	)
	return err
}

func (r *PostgresRepository) GetToken(ctx context.Context, tokenStr string, tokenType string) (*domain.UserToken, error) {
	query := `
		SELECT id, user_id, token, type, used, expires_at, created_at
		FROM user_tokens WHERE token = $1 AND type = $2
	`
	var t domain.UserToken
	err := r.db.QueryRowContext(ctx, query, tokenStr, tokenType).Scan(
		&t.ID, &t.UserID, &t.Token, &t.Type, &t.Used, &t.ExpiresAt, &t.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrInvalidToken
		}
		return nil, err
	}
	return &t, nil
}

func (r *PostgresRepository) MarkTokenUsed(ctx context.Context, id uuid.UUID) error {
	query := `UPDATE user_tokens SET used = TRUE WHERE id = $1`
	_, err := r.db.ExecContext(ctx, query, id)
	return err
}

func (r *PostgresRepository) CreateAuditLog(ctx context.Context, al *domain.AuditLog) error {
	query := `
		INSERT INTO audit_logs (id, actor_id, action, target_resource, target_id, payload, ip_address, user_agent, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	if al.ID == uuid.Nil {
		al.ID = uuid.New()
	}
	al.CreatedAt = time.Now()
	var payloadBytes []byte
	if al.Payload != nil {
		payloadBytes, _ = json.Marshal(al.Payload)
	}

	_, err := r.db.ExecContext(ctx, query,
		al.ID, al.ActorID, al.Action, al.TargetResource, al.TargetID, payloadBytes, al.IPAddress, al.UserAgent, al.CreatedAt,
	)
	return err
}

func (r *PostgresRepository) ListUsers(ctx context.Context, role *domain.Role, status *domain.UserStatus, search string, limit, offset int) ([]*domain.User, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	whereClause := "WHERE 1=1"
	args := []any{}
	argIdx := 1

	if role != nil && *role != "" {
		whereClause += fmt.Sprintf(" AND role = $%d", argIdx)
		args = append(args, string(*role))
		argIdx++
	}
	if status != nil && *status != "" {
		whereClause += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, string(*status))
		argIdx++
	}
	if search != "" {
		whereClause += fmt.Sprintf(" AND (LOWER(email) LIKE $%d OR LOWER(full_name) LIKE $%d)", argIdx, argIdx)
		args = append(args, "%"+strings.ToLower(search)+"%")
		argIdx++
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM users %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count users: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT id, email, password_hash, full_name, role, status, email_verified_at, created_at, updated_at
		FROM users %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d
	`, whereClause, argIdx, argIdx+1)

	args = append(args, limit, offset)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query users: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var users []*domain.User
	for rows.Next() {
		var u domain.User
		var roleStr, statusStr string
		if err := rows.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.FullName, &roleStr, &statusStr, &u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
			return nil, 0, err
		}
		u.Role = domain.Role(roleStr)
		u.Status = domain.UserStatus(statusStr)
		users = append(users, &u)
	}

	return users, total, nil
}

func (r *PostgresRepository) ListAuditLogs(ctx context.Context, limit, offset int) ([]*domain.AuditLog, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := r.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_logs").Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	query := `
		SELECT id, actor_id, action, target_resource, target_id, payload, ip_address, user_agent, created_at
		FROM audit_logs
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.db.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var logs []*domain.AuditLog
	for rows.Next() {
		var al domain.AuditLog
		var payloadBytes []byte
		if err := rows.Scan(&al.ID, &al.ActorID, &al.Action, &al.TargetResource, &al.TargetID, &payloadBytes, &al.IPAddress, &al.UserAgent, &al.CreatedAt); err != nil {
			return nil, 0, err
		}
		if len(payloadBytes) > 0 {
			_ = json.Unmarshal(payloadBytes, &al.Payload)
		}
		logs = append(logs, &al)
	}

	return logs, total, nil
}
