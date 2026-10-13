package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockS3PutObjectAPI struct {
	mock.Mock
}

func (m *mockS3PutObjectAPI) PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	var out *s3.PutObjectOutput
	if o := args.Get(0); o != nil {
		out = o.(*s3.PutObjectOutput)
	}
	return out, args.Error(1)
}

func (m *mockS3PutObjectAPI) HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	var out *s3.HeadObjectOutput
	if o := args.Get(0); o != nil {
		out = o.(*s3.HeadObjectOutput)
	}
	return out, args.Error(1)
}

func TestS3Exists(t *testing.T) {
	const key = "7/0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11"

	t.Run("returns true when the object exists", func(t *testing.T) {
		m := new(mockS3PutObjectAPI)
		m.On("HeadObject", mock.Anything, mock.MatchedBy(func(p *s3.HeadObjectInput) bool {
			return aws.ToString(p.Bucket) == "test-bucket" && aws.ToString(p.Key) == key
		}), mock.Anything).Return(&s3.HeadObjectOutput{}, nil)

		exists, err := NewS3("test-bucket", m).Exists(context.Background(), key)

		require.NoError(t, err)
		require.True(t, exists)
		m.AssertExpectations(t)
	})

	t.Run("returns false without an error when the object is not found", func(t *testing.T) {
		m := new(mockS3PutObjectAPI)
		m.On("HeadObject", mock.Anything, mock.Anything, mock.Anything).Return(nil, &types.NotFound{})

		exists, err := NewS3("test-bucket", m).Exists(context.Background(), key)

		require.NoError(t, err)
		require.False(t, exists)
		m.AssertExpectations(t)
	})

	t.Run("returns other errors wrapped with the key", func(t *testing.T) {
		boom := errors.New("s3 unreachable")
		m := new(mockS3PutObjectAPI)
		m.On("HeadObject", mock.Anything, mock.Anything, mock.Anything).Return(nil, boom)

		exists, err := NewS3("test-bucket", m).Exists(context.Background(), key)

		require.ErrorIs(t, err, boom)
		require.Contains(t, err.Error(), key)
		require.False(t, exists)
		m.AssertExpectations(t)
	})

	t.Run("does not treat other API error codes as not found", func(t *testing.T) {
		apiErr := &smithy.GenericAPIError{Code: "AccessDenied", Message: "denied"}
		m := new(mockS3PutObjectAPI)
		m.On("HeadObject", mock.Anything, mock.Anything, mock.Anything).Return(nil, apiErr)

		exists, err := NewS3("test-bucket", m).Exists(context.Background(), key)

		require.ErrorIs(t, err, apiErr)
		require.False(t, exists)
	})
}

func TestS3Put_Success(t *testing.T) {
	body := "id,name\n1,foo\n"
	sum := sha256.Sum256([]byte(body))
	in := storage.PutInput{
		Key:            "7/0b8e6f1c-3f0a-4a53-9f44-2f6a1d7a8c11",
		Body:           strings.NewReader(body),
		Size:           int64(len(body)),
		ContentType:    "text/csv",
		ChecksumSHA256: sum[:],
	}

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.MatchedBy(func(p *s3.PutObjectInput) bool {
		return aws.ToString(p.Bucket) == "test-bucket" &&
			aws.ToString(p.Key) == in.Key &&
			aws.ToInt64(p.ContentLength) == in.Size &&
			aws.ToString(p.ContentType) == in.ContentType &&
			aws.ToString(p.ChecksumSHA256) == base64.StdEncoding.EncodeToString(sum[:])
	}), mock.Anything).
		Run(func(args mock.Arguments) {
			b, err := io.ReadAll(args.Get(1).(*s3.PutObjectInput).Body)
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(&s3.PutObjectOutput{Size: aws.Int64(in.Size)}, nil)

	err := NewS3("test-bucket", m).Put(context.Background(), in)

	require.NoError(t, err)
	m.AssertExpectations(t)
}

func TestS3Put_ReturnsS3Error(t *testing.T) {
	boom := errors.New("s3 unreachable")

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(nil, boom)

	err := NewS3("test-bucket", m).Put(context.Background(), storage.PutInput{Body: strings.NewReader("x")})

	require.ErrorIs(t, err, boom)
	m.AssertExpectations(t)
}
