package users

import (
	"context"
	"errors"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound           = errors.New("user not found")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrEmailTaken         = errors.New("email is already taken")
	ErrReservedUsername   = errors.New("username is reserved")
	ErrWeakPassword       = errors.New("password must contain an uppercase letter, a lowercase letter, a digit and a special character")
	ErrInvalidCredentials = errors.New("invalid username or password")
)

var reservedUsernames = map[string]struct{}{
	"admin":   {},
	"root":    {},
	"system":  {},
	"support": {},
}

var dummyHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return h
}()

type User struct {
	ID           int64
	Username     string
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Repository interface {
	GetByUsername(ctx context.Context, username string) (User, error)
	Create(ctx context.Context, u User) (User, error)
}

type Service struct {
	repo Repository
}

func New(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetByUsername(ctx context.Context, username string) (User, error) {
	return s.repo.GetByUsername(ctx, username)
}

func (s *Service) Create(ctx context.Context, username, email, password string) (User, error) {
	if _, reserved := reservedUsernames[username]; reserved {
		return User{}, ErrReservedUsername
	}
	if !isStrongPassword(password) {
		return User{}, ErrWeakPassword
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}

	return s.repo.Create(ctx, User{
		Username:     username,
		Email:        email,
		PasswordHash: string(hash),
	})
}

func (s *Service) Authenticate(ctx context.Context, username, password string) (User, error) {
	u, err := s.repo.GetByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// dummy pwd check in order to mitigate timing attacks
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			err = ErrInvalidCredentials
		}
		return User{}, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return User{}, ErrInvalidCredentials
	}
	return u, nil
}

func isStrongPassword(password string) bool {
	var hasUpper, hasLower, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsDigit(r):
			hasDigit = true
		default:
			hasSpecial = true
		}
	}
	return hasUpper && hasLower && hasDigit && hasSpecial
}
