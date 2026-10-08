package api

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aleksandarv/file-uploader/api-service/gen/auth"
	"github.com/aleksandarv/file-uploader/api-service/internal/userservice"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = 15 * time.Minute

type userService interface {
	Authenticate(ctx context.Context, username, password string) (userservice.User, error)
	Create(ctx context.Context, username, email, password string) (userservice.User, error)
}

type authHandler struct {
	secret []byte
	users  userService
}

func NewAuthHandler(secret []byte, users userService) auth.Service {
	return &authHandler{secret: secret, users: users}
}

func (h *authHandler) Login(ctx context.Context, p *auth.LoginPayload) (*auth.LoginResult, error) {
	log := logger.FromCtx(ctx)

	user, err := h.users.Authenticate(ctx, p.Username, p.Password)
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
		Subject:   strconv.FormatInt(user.ID, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(h.secret)
	if err != nil {
		log.Error("failed to sign jwt", "err", err)
		return nil, auth.MakeInternalError(errors.New("failed to issue token"))
	}

	log.Info("login succeeded", "username", p.Username)
	return &auth.LoginResult{Token: signed}, nil
}

func (h *authHandler) Register(ctx context.Context, p *auth.RegisterPayload) (*auth.RegisterResult, error) {
	log := logger.FromCtx(ctx)

	user, err := h.users.Create(ctx, p.Username, p.Email, p.Password)
	switch {
	case errors.Is(err, userservice.ErrInvalidInput):
		log.Error("registration rejected", "username", p.Username, "err", err)
		return nil, auth.MakeInvalidInput(err)
	case errors.Is(err, userservice.ErrUserExists):
		log.Error("registration rejected: user exists", "username", p.Username)
		return nil, auth.MakeUserExists(err)
	case errors.Is(err, userservice.ErrUnavailable):
		log.Error("registration failed: user-service unavailable", "err", err)
		return nil, auth.MakeUnavailable(errors.New("registration is temporarily unavailable"))
	case err != nil:
		log.Error("registration failed: user-service call failed", "err", err)
		return nil, auth.MakeInternalError(errors.New("failed to register user"))
	}

	log.Info("user registered", "username", user.Username, "id", user.ID)
	return &auth.RegisterResult{ID: user.ID, Username: user.Username, Email: user.Email}, nil
}
