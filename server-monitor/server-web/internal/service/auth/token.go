package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid token")
	ErrExpiredToken = errors.New("expired token")
)

type Identity struct {
	ID           uint64 `json:"id"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	TokenVersion int    `json:"token_version"`
}

type TokenClaims struct {
	Subject      string `json:"sub"`
	Username     string `json:"username"`
	Role         string `json:"role"`
	TokenVersion int    `json:"token_version"`
	IssuedAt     int64  `json:"iat"`
	Expires      int64  `json:"exp"`
}

type TokenManager struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

func NewTokenManager(secret string, ttl time.Duration) (*TokenManager, error) {
	trimmed := strings.TrimSpace(secret)
	if len(trimmed) < 32 {
		return nil, fmt.Errorf("jwt secret must be at least 32 bytes, got %d", len(trimmed))
	}
	if ttl <= 0 {
		return nil, errors.New("jwt ttl must be positive")
	}
	return &TokenManager{
		secret: []byte(trimmed),
		ttl:    ttl,
		now:    time.Now,
	}, nil
}

func (m *TokenManager) Generate(identity Identity) (string, time.Time, error) {
	if identity.ID == 0 {
		return "", time.Time{}, errors.New("identity id is required")
	}
	if identity.Username == "" {
		return "", time.Time{}, errors.New("identity username is required")
	}
	if identity.Role == "" {
		return "", time.Time{}, errors.New("identity role is required")
	}

	now := m.now().UTC()
	expiresAt := now.Add(m.ttl)
	header := map[string]string{
		"alg": "HS256",
		"typ": "JWT",
	}
	claims := TokenClaims{
		Subject:      strconv.FormatUint(identity.ID, 10),
		Username:     identity.Username,
		Role:         identity.Role,
		TokenVersion: identity.TokenVersion,
		IssuedAt:     now.Unix(),
		Expires:      expiresAt.Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", time.Time{}, err
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, err
	}

	unsigned := encodeSegment(headerJSON) + "." + encodeSegment(claimsJSON)
	signature := m.sign(unsigned)
	return unsigned + "." + encodeSegment(signature), expiresAt, nil
}

func (m *TokenManager) Parse(token string) (Identity, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Identity{}, ErrInvalidToken
	}

	unsigned := parts[0] + "." + parts[1]
	signature, err := decodeSegment(parts[2])
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	if !hmac.Equal(signature, m.sign(unsigned)) {
		return Identity{}, ErrInvalidToken
	}

	headerBytes, err := decodeSegment(parts[0])
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return Identity{}, ErrInvalidToken
	}
	if header.Alg != "HS256" || header.Typ != "JWT" {
		return Identity{}, ErrInvalidToken
	}

	claimsBytes, err := decodeSegment(parts[1])
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	var claims TokenClaims
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return Identity{}, ErrInvalidToken
	}
	if claims.Expires <= m.now().UTC().Unix() {
		return Identity{}, ErrExpiredToken
	}

	id, err := strconv.ParseUint(claims.Subject, 10, 64)
	if err != nil || id == 0 || claims.Username == "" || claims.Role == "" {
		return Identity{}, ErrInvalidToken
	}
	return Identity{ID: id, Username: claims.Username, Role: claims.Role, TokenVersion: claims.TokenVersion}, nil
}

func (m *TokenManager) sign(unsigned string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(unsigned))
	return mac.Sum(nil)
}

func encodeSegment(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeSegment(data string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(data)
}
