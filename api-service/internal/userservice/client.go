package userservice

import (
	"context"
	"errors"
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
)

type User struct {
	ID       int64
	Username string
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

	return User{ID: res.GetId(), Username: res.GetUsername()}, nil
}
