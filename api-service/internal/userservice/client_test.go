package userservice

import (
	"context"
	"errors"
	"testing"

	userspb "github.com/aleksandarv/file-uploader/user-service/gen/grpc/users/pb"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const (
	testUsername = "john_doe"
	testEmail    = "john.doe@example.com"
	testPassword = "Correct-Horse-42"
)

type mockUsersClient struct {
	mock.Mock
}

func (m *mockUsersClient) GetByUsername(ctx context.Context, in *userspb.GetByUsernameRequest, _ ...grpc.CallOption) (*userspb.GetByUsernameResponse, error) {
	args := m.Called(ctx, in)
	res, _ := args.Get(0).(*userspb.GetByUsernameResponse)
	return res, args.Error(1)
}

func (m *mockUsersClient) Create(ctx context.Context, in *userspb.CreateRequest, _ ...grpc.CallOption) (*userspb.CreateResponse, error) {
	args := m.Called(ctx, in)
	res, _ := args.Get(0).(*userspb.CreateResponse)
	return res, args.Error(1)
}

func (m *mockUsersClient) Authenticate(ctx context.Context, in *userspb.AuthenticateRequest, _ ...grpc.CallOption) (*userspb.AuthenticateResponse, error) {
	args := m.Called(ctx, in)
	res, _ := args.Get(0).(*userspb.AuthenticateResponse)
	return res, args.Error(1)
}

func TestClient_Authenticate(t *testing.T) {
	wantReq := &userspb.AuthenticateRequest{Username: proto.String(testUsername), Password: proto.String(testPassword)}

	t.Run("maps the response to a User", func(t *testing.T) {
		users := &mockUsersClient{}
		users.On("Authenticate", mock.Anything, mock.MatchedBy(func(req *userspb.AuthenticateRequest) bool {
			return proto.Equal(req, wantReq)
		})).Return(&userspb.AuthenticateResponse{Id: proto.Int64(7), Username: proto.String(testUsername), Email: proto.String(testEmail)}, nil)

		got, err := (&Client{users: users}).Authenticate(context.Background(), testUsername, testPassword)

		require.NoError(t, err)
		require.Equal(t, User{ID: 7, Username: testUsername, Email: testEmail}, got)
		users.AssertExpectations(t)
	})

	tests := []struct {
		name    string
		code    codes.Code
		wantErr error
	}{
		{"unauthenticated", codes.Unauthenticated, ErrInvalidCredentials},
		{"invalid argument", codes.InvalidArgument, ErrInvalidCredentials},
		{"unavailable", codes.Unavailable, ErrUnavailable},
		{"deadline exceeded", codes.DeadlineExceeded, ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &mockUsersClient{}
			users.On("Authenticate", mock.Anything, mock.Anything).Return(nil, status.Error(tt.code, "boom"))

			got, err := (&Client{users: users}).Authenticate(context.Background(), testUsername, testPassword)

			require.ErrorIs(t, err, tt.wantErr)
			require.Zero(t, got)
			users.AssertExpectations(t)
		})
	}

	t.Run("passes unexpected errors through", func(t *testing.T) {
		users := &mockUsersClient{}
		users.On("Authenticate", mock.Anything, mock.Anything).Return(nil, status.Error(codes.Internal, "boom"))

		_, err := (&Client{users: users}).Authenticate(context.Background(), testUsername, testPassword)

		require.Equal(t, codes.Internal, status.Code(err))
		require.NotErrorIs(t, err, ErrInvalidCredentials)
		require.NotErrorIs(t, err, ErrUnavailable)
	})
}

func TestClient_Create(t *testing.T) {
	wantReq := &userspb.CreateRequest{
		Username: proto.String(testUsername),
		Email:    proto.String(testEmail),
		Password: proto.String(testPassword),
	}

	t.Run("maps the response to a User", func(t *testing.T) {
		users := &mockUsersClient{}
		users.On("Create", mock.Anything, mock.MatchedBy(func(req *userspb.CreateRequest) bool {
			return proto.Equal(req, wantReq)
		})).Return(&userspb.CreateResponse{Id: proto.Int64(3), Username: proto.String(testUsername), Email: proto.String(testEmail)}, nil)

		got, err := (&Client{users: users}).Create(context.Background(), testUsername, testEmail, testPassword)

		require.NoError(t, err)
		require.Equal(t, User{ID: 3, Username: testUsername, Email: testEmail}, got)
		users.AssertExpectations(t)
	})

	t.Run("keeps the reason for invalid input", func(t *testing.T) {
		users := &mockUsersClient{}
		users.On("Create", mock.Anything, mock.Anything).
			Return(nil, status.Error(codes.InvalidArgument, "password must contain a digit"))

		_, err := (&Client{users: users}).Create(context.Background(), testUsername, testEmail, testPassword)

		require.ErrorIs(t, err, ErrInvalidInput)
		require.ErrorContains(t, err, "password must contain a digit")
	})

	tests := []struct {
		name    string
		code    codes.Code
		wantErr error
	}{
		{"already exists", codes.AlreadyExists, ErrUserExists},
		{"unavailable", codes.Unavailable, ErrUnavailable},
		{"deadline exceeded", codes.DeadlineExceeded, ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &mockUsersClient{}
			users.On("Create", mock.Anything, mock.Anything).Return(nil, status.Error(tt.code, "boom"))

			got, err := (&Client{users: users}).Create(context.Background(), testUsername, testEmail, testPassword)

			require.ErrorIs(t, err, tt.wantErr)
			require.Zero(t, got)
			users.AssertExpectations(t)
		})
	}

	t.Run("passes unexpected errors through", func(t *testing.T) {
		users := &mockUsersClient{}
		users.On("Create", mock.Anything, mock.Anything).Return(nil, errors.New("plain error"))

		_, err := (&Client{users: users}).Create(context.Background(), testUsername, testEmail, testPassword)

		require.EqualError(t, err, "plain error")
	})
}
