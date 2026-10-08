package storage

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
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

func TestS3Upload_Success(t *testing.T) {
	body := "id,name\n1,foo\n"
	sum := sha256.Sum256([]byte(body))
	checksumB64 := base64.StdEncoding.EncodeToString(sum[:])

	in := UploadInput{
		UserID:      7,
		UUID:        uuid.NewString(),
		Filename:    "users.csv",
		ContentType: "text/csv",
		Size:        int64(len(body)),
		Checksum:    hex.EncodeToString(sum[:]),
		Body:        strings.NewReader(body),
	}

	var gotKey string
	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.MatchedBy(func(p *s3.PutObjectInput) bool {
		return aws.ToString(p.Bucket) == "test-bucket" &&
			aws.ToInt64(p.ContentLength) == in.Size &&
			aws.ToString(p.ContentType) == in.ContentType &&
			aws.ToString(p.ChecksumSHA256) == checksumB64
	}), mock.Anything).
		Run(func(args mock.Arguments) {
			put := args.Get(1).(*s3.PutObjectInput)
			gotKey = aws.ToString(put.Key)
			b, err := io.ReadAll(put.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(&s3.PutObjectOutput{Size: aws.Int64(in.Size)}, nil)

	res, err := NewS3("test-bucket", m).Upload(context.Background(), in)

	require.NoError(t, err)
	require.Equal(t, in.Filename, res.Filename)
	require.Equal(t, in.ContentType, res.ContentType)
	require.Equal(t, in.Size, res.Size)
	require.Equal(t, in.UUID, res.UUID)
	require.Equal(t, "7/"+in.UUID, gotKey)
	m.AssertExpectations(t)
}

func TestS3Upload_ReturnsS3Error(t *testing.T) {
	boom := errors.New("s3 unreachable")

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).Return(nil, boom)

	_, err := NewS3("test-bucket", m).Upload(context.Background(), UploadInput{Body: strings.NewReader("x")})

	require.ErrorIs(t, err, boom)
	m.AssertExpectations(t)
}

func TestS3Upload_RejectsMalformedChecksum(t *testing.T) {
	m := new(mockS3PutObjectAPI)

	_, err := NewS3("test-bucket", m).Upload(context.Background(), UploadInput{Checksum: "not-hex", Body: strings.NewReader("x")})

	require.ErrorIs(t, err, ErrInvalidChecksum)
	m.AssertNotCalled(t, "PutObject", mock.Anything, mock.Anything, mock.Anything)
}
