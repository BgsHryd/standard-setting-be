package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"standard-setting/backend/internal/domain"
)

type Claims struct {
	AuthVersion        int         `json:"authVersion"`
	Name               string      `json:"name"`
	Username           string      `json:"username"`
	Role               domain.Role `json:"role"`
	ProjectID          string      `json:"projectId,omitempty"`
	SubjectCode        string      `json:"subjectCode,omitempty"`
	MustChangePassword bool        `json:"mustChangePassword"`
	jwt.RegisteredClaims
}

func (s *Service) issueAccessToken(user domain.User) (string, error) {
	now := time.Now()
	claims := Claims{
		AuthVersion: user.AuthVersion, Name: user.Name, Username: user.Username, Role: user.Role, ProjectID: user.ProjectID,
		SubjectCode: user.SubjectCode, MustChangePassword: user.MustChangePassword,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: user.ID, Issuer: "standard-setting", IssuedAt: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.accessTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
}

func (s *Service) ParseAccessToken(raw string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(raw, &Claims{}, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("algoritma token tidak valid")
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return nil, ErrInvalidSession
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return nil, ErrInvalidSession
	}
	return claims, nil
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
