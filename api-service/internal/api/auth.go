package api

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/aleksandarv/file-uploader/api-service/gen/auth"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = 15 * time.Minute

// TODO: replace with a call to user-service once it owns credential storage.
const (
	hardcodedUsername = "admin123"
	hardcodedPassword = "admin123"
)

type authSvc struct {
	secret []byte
}

func NewAuthSvc(secret []byte) auth.Service {
	return &authSvc{secret: secret}
}

func (s *authSvc) Login(ctx context.Context, p *auth.LoginPayload) (*auth.LoginResult, error) {
	log := logger.FromCtx(ctx)

	validUsername := subtle.ConstantTimeCompare([]byte(p.Username), []byte(hardcodedUsername)) == 1
	validPassword := subtle.ConstantTimeCompare([]byte(p.Password), []byte(hardcodedPassword)) == 1
	if !validUsername || !validPassword {
		log.Warn("login failed: invalid credentials", "username", p.Username)
		return nil, auth.MakeInvalidCredentials(errors.New("invalid username or password"))
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   p.Username,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		log.Error("failed to sign jwt", "err", err)
		return nil, auth.MakeInternalError(errors.New("failed to issue token"))
	}

	log.Info("login succeeded", "username", p.Username)
	return &auth.LoginResult{Token: signed}, nil
}
