package userservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	userspb "github.com/aleksandarv/file-uploader/user-service/gen/grpc/users/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const callTimeout = 5 * time.Second

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUnavailable        = errors.New("user-service unavailable")
	ErrUserExists         = errors.New("user already exists")
	ErrInvalidInput       = errors.New("invalid user data")
)

type User struct {
	ID       int64
	Username string
	Email    string
}

type Client struct {
	users userspb.UsersClient
}

func NewClient(cc grpc.ClientConnInterface) *Client {
	return &Client{users: userspb.NewUsersClient(cc)}
}

func (c *Client) Authenticate(ctx context.Context, username, password string) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	res, err := c.users.Authenticate(ctx, &userspb.AuthenticateRequest{
		Username: &username,
		Password: &password,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.Unauthenticated, codes.InvalidArgument:
			return User{}, ErrInvalidCredentials
		case codes.Unavailable, codes.DeadlineExceeded:
			return User{}, errors.Join(ErrUnavailable, err)
		default:
			return User{}, err
		}
	}

	return User{ID: res.GetId(), Username: res.GetUsername(), Email: res.GetEmail()}, nil
}

func (c *Client) Create(ctx context.Context, username, email, password string) (User, error) {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()

	res, err := c.users.Create(ctx, &userspb.CreateRequest{
		Username: &username,
		Email:    &email,
		Password: &password,
	})
	if err != nil {
		switch status.Code(err) {
		case codes.InvalidArgument:
			return User{}, fmt.Errorf("%w: %s", ErrInvalidInput, status.Convert(err).Message())
		case codes.AlreadyExists:
			return User{}, ErrUserExists
		case codes.Unavailable, codes.DeadlineExceeded:
			return User{}, errors.Join(ErrUnavailable, err)
		default:
			return User{}, err
		}
	}

	return User{ID: res.GetId(), Username: res.GetUsername(), Email: res.GetEmail()}, nil
}
