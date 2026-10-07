package api

import (
	"context"
	"errors"
	"time"

	"github.com/aleksandarv/file-uploader/api-service/gen/auth"
	"github.com/aleksandarv/file-uploader/api-service/internal/userservice"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = 15 * time.Minute

type authenticator interface {
	Authenticate(ctx context.Context, username, password string) (userservice.User, error)
}

type authSvc struct {
	secret []byte
	users  authenticator
}

func NewAuthSvc(secret []byte, users authenticator) auth.Service {
	return &authSvc{secret: secret, users: users}
}

func (s *authSvc) Login(ctx context.Context, p *auth.LoginPayload) (*auth.LoginResult, error) {
	log := logger.FromCtx(ctx)

	user, err := s.users.Authenticate(ctx, p.Username, p.Password)
	switch {
	case errors.Is(err, userservice.ErrInvalidCredentials):
		log.Error("login failed: invalid credentials", "username", p.Username)
		return nil, auth.MakeInvalidCredentials(err)
	case errors.Is(err, userservice.ErrUnavailable):
		log.Error("login failed: user-service unavailable", "err", err)
		return nil, auth.MakeUnavailable(errors.New("authentication is temporarily unavailable"))
	case err != nil:
		log.Error("login failed: user-service call failed", "err", err)
		return nil, auth.MakeInternalError(errors.New("failed to authenticate"))
	}

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   user.Username,
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
