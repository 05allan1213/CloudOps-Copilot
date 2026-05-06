package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"server-web/model"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrAuthUnavailable    = errors.New("auth service unavailable")
	ErrBearerTokenMissing = errors.New("bearer token required")
	ErrUsernameInvalid    = errors.New("username must be 3-64 characters, letters, digits and underscores only")
	ErrPasswordTooShort   = errors.New("password must be at least 8 characters")
	ErrRoleInvalid        = errors.New("role must be admin or viewer")
	ErrUserExists         = errors.New("username already exists")
	ErrTokenRevoked       = errors.New("token has been revoked")
)

type Service struct {
	db     *gorm.DB
	tokens *TokenManager
}

type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	User      Identity
}

func NewService(db *gorm.DB, jwtSecret string, tokenTTL time.Duration) (*Service, error) {
	if db == nil {
		return nil, ErrAuthUnavailable
	}
	tokens, err := NewTokenManager(jwtSecret, tokenTTL)
	if err != nil {
		return nil, err
	}
	return &Service{db: db, tokens: tokens}, nil
}

func (s *Service) EnsureInitialAdmin(ctx context.Context, adminPassword string) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.User{}).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 || strings.TrimSpace(adminPassword) == "" {
		return false, nil
	}

	hashedPassword, err := HashPassword(adminPassword)
	if err != nil {
		return false, err
	}
	admin := model.User{
		Username: "admin",
		Password: hashedPassword,
		Role:     "admin",
	}
	if err := s.db.WithContext(ctx).Create(&admin).Error; err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) Login(ctx context.Context, username string, password string) (LoginResult, error) {
	username = strings.TrimSpace(username)
	if username == "" || password == "" {
		return LoginResult{}, ErrInvalidCredentials
	}

	var user model.User
	err := s.db.WithContext(ctx).Where("username = ?", username).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return LoginResult{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, err
	}
	if !VerifyPassword(user.Password, password) {
		return LoginResult{}, ErrInvalidCredentials
	}

	identity := Identity{
		ID:           user.ID,
		Username:     user.Username,
		Role:         user.Role,
		TokenVersion: user.TokenVersion,
	}
	token, expiresAt, err := s.tokens.Generate(identity)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{
		Token:     token,
		ExpiresAt: expiresAt,
		User:      identity,
	}, nil
}

func (s *Service) AuthenticateBearer(authHeader string) (Identity, error) {
	const prefix = "Bearer "
	if !strings.HasPrefix(authHeader, prefix) {
		return Identity{}, ErrBearerTokenMissing
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, prefix))
	if token == "" {
		return Identity{}, ErrBearerTokenMissing
	}
	return s.tokens.Parse(token)
}

func (s *Service) AuthenticateToken(token string) (Identity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Identity{}, ErrBearerTokenMissing
	}
	return s.tokens.Parse(token)
}

func (s *Service) VerifyTokenVersion(ctx context.Context, identity Identity) error {
	var user model.User
	if err := s.db.WithContext(ctx).Select("id, token_version").First(&user, identity.ID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrTokenRevoked
		}
		return err
	}
	if user.TokenVersion != identity.TokenVersion {
		return ErrTokenRevoked
	}
	return nil
}

func (s *Service) Register(ctx context.Context, username, password, role string) (Identity, error) {
	username = strings.TrimSpace(username)
	if !isValidUsername(username) {
		return Identity{}, ErrUsernameInvalid
	}
	if len(password) < 8 {
		return Identity{}, ErrPasswordTooShort
	}
	role = strings.TrimSpace(strings.ToLower(role))
	if role != "admin" && role != "viewer" {
		return Identity{}, ErrRoleInvalid
	}

	hashedPassword, err := HashPassword(password)
	if err != nil {
		return Identity{}, err
	}

	user := model.User{
		Username: username,
		Password: hashedPassword,
		Role:     role,
	}
	if err := s.db.WithContext(ctx).Create(&user).Error; err != nil {
		if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "1062") {
			return Identity{}, ErrUserExists
		}
		return Identity{}, err
	}

	return Identity{ID: user.ID, Username: user.Username, Role: user.Role, TokenVersion: user.TokenVersion}, nil
}

func (s *Service) ListUsers(ctx context.Context) ([]Identity, error) {
	var users []model.User
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&users).Error; err != nil {
		return nil, err
	}
	result := make([]Identity, 0, len(users))
	for _, u := range users {
		result = append(result, Identity{ID: u.ID, Username: u.Username, Role: u.Role, TokenVersion: u.TokenVersion})
	}
	return result, nil
}

func (s *Service) DeleteUser(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&model.User{}, id).Error
}

func isValidUsername(username string) bool {
	if len(username) < 3 || len(username) > 64 {
		return false
	}
	for _, c := range username {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}
