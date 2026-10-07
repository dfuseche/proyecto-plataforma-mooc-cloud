package user

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mooc-platform/backend/internal/domain"
	"golang.org/x/crypto/bcrypt"
)

type mockUserRepo struct {
	users        map[string]*domain.User
	usersByID    map[uuid.UUID]*domain.User
	sessions     map[string]*domain.UserSession
	tokens       map[string]*domain.UserToken
	auditLogs    []*domain.AuditLog
	activeAdmins int
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users:        make(map[string]*domain.User),
		usersByID:    make(map[uuid.UUID]*domain.User),
		sessions:     make(map[string]*domain.UserSession),
		tokens:       make(map[string]*domain.UserToken),
		auditLogs:    make([]*domain.AuditLog, 0),
		activeAdmins: 1,
	}
}

func (m *mockUserRepo) CreateUser(ctx context.Context, u *domain.User) error {
	m.users[u.Email] = u
	m.usersByID[u.ID] = u
	return nil
}

func (m *mockUserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	u, ok := m.usersByID[id]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return u, nil
}

func (m *mockUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	u, ok := m.users[email]
	if !ok {
		return nil, domain.ErrUserNotFound
	}
	return u, nil
}

func (m *mockUserRepo) UpdateUser(ctx context.Context, u *domain.User) error {
	m.users[u.Email] = u
	m.usersByID[u.ID] = u
	return nil
}

func (m *mockUserRepo) CountActiveAdmins(ctx context.Context) (int, error) {
	return m.activeAdmins, nil
}

func (m *mockUserRepo) CreateSession(ctx context.Context, s *domain.UserSession) error {
	m.sessions[s.Token] = s
	return nil
}

func (m *mockUserRepo) GetSessionByToken(ctx context.Context, token string) (*domain.UserSession, error) {
	s, ok := m.sessions[token]
	if !ok {
		return nil, domain.ErrSessionExpired
	}
	return s, nil
}

func (m *mockUserRepo) RevokeSession(ctx context.Context, sessionID uuid.UUID) error {
	for _, s := range m.sessions {
		if s.ID == sessionID {
			s.IsRevoked = true
		}
	}
	return nil
}

func (m *mockUserRepo) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	for _, s := range m.sessions {
		if s.UserID == userID {
			s.IsRevoked = true
		}
	}
	return nil
}

func (m *mockUserRepo) CreateToken(ctx context.Context, t *domain.UserToken) error {
	m.tokens[t.Token] = t
	return nil
}

func (m *mockUserRepo) GetToken(ctx context.Context, tokenStr string, tokenType string) (*domain.UserToken, error) {
	t, ok := m.tokens[tokenStr]
	if !ok || t.Type != tokenType {
		return nil, domain.ErrInvalidToken
	}
	return t, nil
}

func (m *mockUserRepo) MarkTokenUsed(ctx context.Context, id uuid.UUID) error {
	for _, t := range m.tokens {
		if t.ID == id {
			t.Used = true
		}
	}
	return nil
}

func (m *mockUserRepo) CreateAuditLog(ctx context.Context, al *domain.AuditLog) error {
	m.auditLogs = append(m.auditLogs, al)
	return nil
}

func (m *mockUserRepo) ListUsers(ctx context.Context, role *domain.Role, status *domain.UserStatus, search string, limit, offset int) ([]*domain.User, int, error) {
	res := make([]*domain.User, 0)
	for _, u := range m.usersByID {
		res = append(res, u)
	}
	return res, len(res), nil
}

func (m *mockUserRepo) ListAuditLogs(ctx context.Context, limit, offset int) ([]*domain.AuditLog, int, error) {
	return m.auditLogs, len(m.auditLogs), nil
}

func TestRegisterAndVerifyStudent(t *testing.T) {
	repo := newMockUserRepo()
	uc := NewUseCase(repo)
	ctx := context.Background()

	// 1. Registro
	user, token, err := uc.RegisterStudent(ctx, RegisterStudentInput{
		Email:    "estudiante@test.com",
		Password: "password123",
		FullName: "Estudiante Prueba",
	})
	if err != nil {
		t.Fatalf("Registro falló inesperadamente: %v", err)
	}
	if user.Status != domain.StatusUnverified {
		t.Errorf("Esperaba estado unverified, obtenido: %s", user.Status)
	}
	if token == "" {
		t.Error("Esperaba token de verificación no vacío")
	}

	// 2. Intento de login previo a verificación (debe fallar)
	_, err = uc.Login(ctx, LoginInput{
		Email:    "estudiante@test.com",
		Password: "password123",
	})
	if err != domain.ErrEmailNotVerified {
		t.Errorf("Esperaba ErrEmailNotVerified, obtenido: %v", err)
	}

	// 3. Verificación de correo
	err = uc.VerifyEmail(ctx, token)
	if err != nil {
		t.Fatalf("Verificación de correo falló: %v", err)
	}

	// 4. Login posterior a verificación (debe ser exitoso)
	loginOut, err := uc.Login(ctx, LoginInput{
		Email:    "estudiante@test.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login post-verificación falló: %v", err)
	}
	if loginOut.Session.Token == "" {
		t.Error("Sesión no generada correctamente")
	}
}

func TestLastAdminProtection(t *testing.T) {
	repo := newMockUserRepo()
	uc := NewUseCase(repo)
	ctx := context.Background()

	adminID := uuid.New()
	hash, _ := bcrypt.GenerateFromPassword([]byte("adminpass"), bcrypt.DefaultCost)
	now := time.Now()
	adminUser := &domain.User{
		ID:              adminID,
		Email:           "admin@test.com",
		PasswordHash:    string(hash),
		FullName:        "Admin Principal",
		Role:            domain.RoleAdmin,
		Status:          domain.StatusActive,
		EmailVerifiedAt: &now,
	}
	if err := repo.CreateUser(ctx, adminUser); err != nil {
		t.Fatalf("setup: CreateUser falló: %v", err)
	}
	repo.activeAdmins = 1

	// Intentar suspender al único administrador activo
	err := uc.ChangeUserStatus(ctx, adminID, adminID, domain.StatusSuspended)
	if err != domain.ErrLastAdminProtection {
		t.Errorf("Esperaba ErrLastAdminProtection, obtenido: %v", err)
	}
}
