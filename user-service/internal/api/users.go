package api

import (
	"context"
	"errors"
	"time"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/user-service/gen/users"
	userssvc "github.com/aleksandarv/file-uploader/user-service/internal/service/users"
)

type usersHandler struct {
	svc *userssvc.Service
}

func NewUsersHandler(svc *userssvc.Service) users.Service {
	return &usersHandler{svc: svc}
}

func (h *usersHandler) GetByUsername(ctx context.Context, p *users.GetByUsernamePayload) (*users.User, error) {
	log := logger.FromCtx(ctx)

	u, err := h.svc.GetByUsername(ctx, p.Username)
	if errors.Is(err, userssvc.ErrNotFound) {
		return nil, users.MakeNotFound(err)
	}
	if err != nil {
		log.Error("failed to get user", "username", p.Username, "err", err)
		return nil, users.MakeInternalError(errors.New("failed to get user"))
	}

	return toUser(u), nil
}

func (h *usersHandler) Create(ctx context.Context, p *users.CreatePayload) (*users.User, error) {
	log := logger.FromCtx(ctx)

	u, err := h.svc.Create(ctx, p.Username, p.Email, p.Password)
	switch {
	case errors.Is(err, userssvc.ErrReservedUsername), errors.Is(err, userssvc.ErrWeakPassword):
		return nil, users.MakeBadRequest(err)
	case errors.Is(err, userssvc.ErrUsernameTaken), errors.Is(err, userssvc.ErrEmailTaken):
		return nil, users.MakeAlreadyExists(err)
	case err != nil:
		log.Error("failed to create user", "username", p.Username, "err", err)
		return nil, users.MakeInternalError(errors.New("failed to create user"))
	}

	log.Info("user created", "username", u.Username, "id", u.ID)
	return toUser(u), nil
}

func (h *usersHandler) Authenticate(ctx context.Context, p *users.AuthenticatePayload) (*users.User, error) {
	log := logger.FromCtx(ctx)

	u, err := h.svc.Authenticate(ctx, p.Username, p.Password)
	if errors.Is(err, userssvc.ErrInvalidCredentials) {
		log.Error("authentication failed", "username", p.Username)
		return nil, users.MakeUnauthorized(err)
	}
	if err != nil {
		log.Error("failed to authenticate", "username", p.Username, "err", err)
		return nil, users.MakeInternalError(errors.New("failed to authenticate"))
	}

	return toUser(u), nil
}

func toUser(u userssvc.User) *users.User {
	return &users.User{
		ID:        u.ID,
		Username:  u.Username,
		Email:     u.Email,
		CreatedAt: u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: u.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
