package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/file-service/gen/files"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
)

func testCtx() context.Context {
	return logger.WithCtx(context.Background(), logger.NewLogger(false))
}

func checksumOf(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

func loadTestdata(t *testing.T, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(b)
}

type mockReadCloser struct {
	io.Reader
	mock.Mock
}

func (m *mockReadCloser) Close() error {
	return m.Called().Error(0)
}

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

func TestFilesService_Upload_Success(t *testing.T) {
	body := "id,name\n1,foo\n"
	checksum := checksumOf(body)
	checksumB64 := func() string {
		raw, err := hex.DecodeString(checksum)
		require.NoError(t, err)
		return base64.StdEncoding.EncodeToString(raw)
	}()

	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksum,
	}

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.MatchedBy(func(in *s3.PutObjectInput) bool {
		return aws.ToString(in.Bucket) == "test-bucket" &&
			aws.ToString(in.Key) == payload.Filename &&
			aws.ToInt64(in.ContentLength) == payload.Size &&
			aws.ToString(in.ContentType) == payload.ContentType &&
			aws.ToString(in.ChecksumSHA256) == checksumB64
	}), mock.Anything).
		Run(func(args mock.Arguments) {
			in := args.Get(1).(*s3.PutObjectInput)
			b, err := io.ReadAll(in.Body)
			require.NoError(t, err)
			require.Equal(t, body, string(b))
		}).
		Return(&s3.PutObjectOutput{
			ChecksumSHA256: aws.String(checksumB64),
			Size:           aws.Int64(payload.Size),
		}, nil)

	svc := NewFilesService("test-bucket", m)
	err := svc.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.NoError(t, err)
	m.AssertExpectations(t)
}

func TestFilesService_Upload_RejectsMalformedChecksum(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    "not-hex",
	}

	m := new(mockS3PutObjectAPI)

	svc := NewFilesService("test-bucket", m)
	err := svc.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.Error(t, err)

	var svcErr *goa.ServiceError
	require.True(t, errors.As(err, &svcErr))
	require.Equal(t, "bad_request", svcErr.Name)
	m.AssertNotCalled(t, "PutObject", mock.Anything, mock.Anything, mock.Anything)
}

func TestFilesService_Upload_WrapsS3ErrorAsInternalError(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("s3 unreachable"))

	svc := NewFilesService("test-bucket", m)
	err := svc.Upload(testCtx(), payload, io.NopCloser(strings.NewReader(body)))

	require.Error(t, err)

	var svcErr *goa.ServiceError
	require.True(t, errors.As(err, &svcErr))
	require.Equal(t, "internal_error", svcErr.Name)
	require.Equal(t, "Error while storing file", svcErr.Message)
	m.AssertExpectations(t)
}

func TestFilesService_Upload_ClosesBody(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	rc := &mockReadCloser{Reader: strings.NewReader(body)}
	rc.On("Close").Return(nil)

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).
		Return(&s3.PutObjectOutput{}, nil)

	svc := NewFilesService("test-bucket", m)
	require.NoError(t, svc.Upload(testCtx(), payload, rc))
	rc.AssertExpectations(t)
}

func TestFilesService_Upload_ClosesBodyOnMalformedChecksum(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    "not-hex",
	}

	rc := &mockReadCloser{Reader: strings.NewReader(body)}
	rc.On("Close").Return(nil)

	m := new(mockS3PutObjectAPI)

	svc := NewFilesService("test-bucket", m)
	require.Error(t, svc.Upload(testCtx(), payload, rc))
	rc.AssertExpectations(t)
}

func TestFilesService_Upload_ClosesBodyOnS3Failure(t *testing.T) {
	body := "id,name\n1,foo\n"
	payload := &files.UploadPayload{
		Filename:    "users.csv",
		Size:        int64(len(body)),
		ContentType: "text/csv",
		Checksum:    checksumOf(body),
	}

	rc := &mockReadCloser{Reader: strings.NewReader(body)}
	rc.On("Close").Return(nil)

	m := new(mockS3PutObjectAPI)
	m.On("PutObject", mock.Anything, mock.Anything, mock.Anything).
		Return(nil, errors.New("s3 unreachable"))

	svc := NewFilesService("test-bucket", m)
	require.Error(t, svc.Upload(testCtx(), payload, rc))
	rc.AssertExpectations(t)
}
