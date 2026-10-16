//go:build integration

package api

import (
	"context"
	"strings"
	"testing"

	userspb "github.com/aleksandarv/file-uploader/user-service/gen/grpc/users/pb"
	userssvc "github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestUsersGRPC_Authenticate_Success(t *testing.T) {
	t.Parallel()

	client := newGRPCClient(t, returning(storedUser(t), nil))

	res, err := client.Authenticate(context.Background(), &userspb.AuthenticateRequest{
		Username: strPtr(testUsername),
		Password: strPtr(testPassword),
	})

	require.NoError(t, err)
	require.Equal(t, testUsername, res.GetUsername())
	require.Equal(t, int64(9), res.GetId())
}

func TestUsersGRPC_Authenticate_MapsErrorsToStatusCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		repo     *mockRepo
		username string
		password string
		want     codes.Code
	}{
		{"wrong password", returning(storedUser(t), nil), testUsername, "Wrong-Password-1", codes.Unauthenticated},
		{"unknown user", returning(userssvc.User{}, userssvc.ErrNotFound), testUsername, testPassword, codes.Unauthenticated},
		{"username violates design pattern", returning(storedUser(t), nil), "John", testPassword, codes.InvalidArgument},
		{"empty password violates design", returning(storedUser(t), nil), testUsername, "", codes.InvalidArgument},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newGRPCClient(t, tt.repo)

			_, err := client.Authenticate(context.Background(), &userspb.AuthenticateRequest{
				Username: strPtr(tt.username),
				Password: strPtr(tt.password),
			})

			require.Equal(t, tt.want, status.Code(err), "err: %v", err)
		})
	}
}

func TestUsersGRPC_Create_EnforcesDesignValidation(t *testing.T) {
	t.Parallel()

	client := newGRPCClient(t, returning(storedUser(t), nil))

	tests := []struct {
		name      string
		username  string
		email     string
		firstName *string
		lastName  *string
		password  string
	}{
		{"uppercase username", "John", testEmail, strPtr(testFirstName), strPtr(testLastName), testPassword},
		{"username starting with digit", "1john", testEmail, strPtr(testFirstName), strPtr(testLastName), testPassword},
		{"short username", "jo", testEmail, strPtr(testFirstName), strPtr(testLastName), testPassword},
		{"invalid email", testUsername, "not-an-email", strPtr(testFirstName), strPtr(testLastName), testPassword},
		{"short password", testUsername, testEmail, strPtr(testFirstName), strPtr(testLastName), "Ab1!"},
		{"password with emoji", testUsername, testEmail, strPtr(testFirstName), strPtr(testLastName), "Correct-Horse-42😀"},
		{"password with space", testUsername, testEmail, strPtr(testFirstName), strPtr(testLastName), "Correct Horse 42"},
		{"missing first name", testUsername, testEmail, nil, strPtr(testLastName), testPassword},
		{"missing last name", testUsername, testEmail, strPtr(testFirstName), nil, testPassword},
		{"empty first name", testUsername, testEmail, strPtr(""), strPtr(testLastName), testPassword},
		{"empty last name", testUsername, testEmail, strPtr(testFirstName), strPtr(""), testPassword},
		{"first name longer than 100 characters", testUsername, testEmail, strPtr(strings.Repeat("a", 101)), strPtr(testLastName), testPassword},
		{"last name longer than 100 characters", testUsername, testEmail, strPtr(testFirstName), strPtr(strings.Repeat("a", 101)), testPassword},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := client.Create(context.Background(), &userspb.CreateRequest{
				Username:  strPtr(tt.username),
				Email:     strPtr(tt.email),
				FirstName: tt.firstName,
				LastName:  tt.lastName,
				Password:  strPtr(tt.password),
			})

			require.Equal(t, codes.InvalidArgument, status.Code(err), "err: %v", err)
		})
	}
}

func TestUsersGRPC_Create_Conflict(t *testing.T) {
	t.Parallel()

	client := newGRPCClient(t, returning(userssvc.User{}, userssvc.ErrUsernameTaken))

	_, err := client.Create(context.Background(), &userspb.CreateRequest{
		Username:  strPtr(testUsername),
		Email:     strPtr(testEmail),
		FirstName: strPtr(testFirstName),
		LastName:  strPtr(testLastName),
		Password:  strPtr(testPassword),
	})

	require.Equal(t, codes.AlreadyExists, status.Code(err))
}

func TestUsersGRPC_Create_ReturnsNames(t *testing.T) {
	t.Parallel()

	client := newGRPCClient(t, returning(storedUser(t), nil))

	res, err := client.Create(context.Background(), &userspb.CreateRequest{
		Username:  strPtr(testUsername),
		Email:     strPtr(testEmail),
		FirstName: strPtr(testFirstName),
		LastName:  strPtr(testLastName),
		Password:  strPtr(testPassword),
	})

	require.NoError(t, err)
	require.Equal(t, testFirstName, res.GetFirstName())
	require.Equal(t, testLastName, res.GetLastName())
}

func TestUsersGRPC_GetByUsername_NotFound(t *testing.T) {
	t.Parallel()

	client := newGRPCClient(t, returning(userssvc.User{}, userssvc.ErrNotFound))

	_, err := client.GetByUsername(context.Background(), &userspb.GetByUsernameRequest{Username: strPtr(testUsername)})

	require.Equal(t, codes.NotFound, status.Code(err))
}
