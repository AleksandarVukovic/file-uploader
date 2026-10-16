package objectstore

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/file-processing-service/internal/service/processor"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testBucket      = "test-bucket"
	otherTestBucket = "other-test-bucket"
	testKey         = "42/0b9f6c1e-5c0e-4f55-9d0e-3f1d5f0f9a11"
)

type mockS3GetObjectAPI struct {
	mock.Mock
}

func (m *mockS3GetObjectAPI) GetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error) {
	args := m.Called(ctx, params, optFns)
	var out *awss3.GetObjectOutput
	if o := args.Get(0); o != nil {
		out = o.(*awss3.GetObjectOutput)
	}
	return out, args.Error(1)
}

func matchesObject(bucket string) any {
	return mock.MatchedBy(func(in *awss3.GetObjectInput) bool {
		return aws.ToString(in.Bucket) == bucket && aws.ToString(in.Key) == testKey
	})
}

func TestGet_ReturnsObjectBody(t *testing.T) {
	api := &mockS3GetObjectAPI{}
	api.On("GetObject", mock.Anything, matchesObject(testBucket), mock.Anything).
		Return(&awss3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("hello"))}, error(nil))
	store := NewS3([]string{testBucket, otherTestBucket}, api)

	body, err := store.Get(context.Background(), testBucket, testKey)

	require.NoError(t, err)
	defer body.Close()
	got, err := io.ReadAll(body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(got))
	api.AssertExpectations(t)
}

func TestGet_ReadsFromTheBucketRequestedAmongAllowedOnes(t *testing.T) {
	api := &mockS3GetObjectAPI{}
	api.On("GetObject", mock.Anything, matchesObject(otherTestBucket), mock.Anything).
		Return(&awss3.GetObjectOutput{Body: io.NopCloser(strings.NewReader("hello"))}, error(nil))
	store := NewS3([]string{testBucket, otherTestBucket}, api)

	body, err := store.Get(context.Background(), otherTestBucket, testKey)

	require.NoError(t, err)
	require.NoError(t, body.Close())
	api.AssertExpectations(t)
}

func TestGet_NoSuchKey_ReturnsErrObjectNotFound(t *testing.T) {
	api := &mockS3GetObjectAPI{}
	api.On("GetObject", mock.Anything, matchesObject(testBucket), mock.Anything).
		Return(nil, &types.NoSuchKey{})
	store := NewS3([]string{testBucket}, api)

	body, err := store.Get(context.Background(), testBucket, testKey)

	require.Nil(t, body)
	require.ErrorIs(t, err, processor.ErrObjectNotFound)
	api.AssertExpectations(t)
}

func TestGet_BucketOutsideAllowlist_ReturnsErrBucketNotAllowedWithoutCallingS3(t *testing.T) {
	api := &mockS3GetObjectAPI{}
	store := NewS3([]string{testBucket, otherTestBucket}, api)

	body, err := store.Get(context.Background(), "unknown-bucket", testKey)

	require.Nil(t, body)
	require.ErrorIs(t, err, processor.ErrBucketNotAllowed)
	require.ErrorContains(t, err, "unknown-bucket")
	require.ErrorContains(t, err, testBucket)
	require.ErrorContains(t, err, otherTestBucket)
	api.AssertNotCalled(t, "GetObject", mock.Anything, mock.Anything, mock.Anything)
}

func TestGet_EmptyAllowlist_RejectsEveryBucket(t *testing.T) {
	api := &mockS3GetObjectAPI{}
	store := NewS3(nil, api)

	body, err := store.Get(context.Background(), testBucket, testKey)

	require.Nil(t, body)
	require.ErrorIs(t, err, processor.ErrBucketNotAllowed)
	api.AssertNotCalled(t, "GetObject", mock.Anything, mock.Anything, mock.Anything)
}

func TestNewS3_CopiesAllowlist(t *testing.T) {
	api := &mockS3GetObjectAPI{}
	allowed := []string{testBucket}
	store := NewS3(allowed, api)
	allowed[0] = "unknown-bucket"

	body, err := store.Get(context.Background(), "unknown-bucket", testKey)

	require.Nil(t, body)
	require.ErrorIs(t, err, processor.ErrBucketNotAllowed)
	api.AssertNotCalled(t, "GetObject", mock.Anything, mock.Anything, mock.Anything)
}

func TestGet_OtherError_IsWrappedAndNotMappedToNotFound(t *testing.T) {
	apiErr := errors.New("s3 unavailable")
	api := &mockS3GetObjectAPI{}
	api.On("GetObject", mock.Anything, matchesObject(testBucket), mock.Anything).
		Return(nil, apiErr)
	store := NewS3([]string{testBucket}, api)

	body, err := store.Get(context.Background(), testBucket, testKey)

	require.Nil(t, body)
	require.ErrorIs(t, err, apiErr)
	require.NotErrorIs(t, err, processor.ErrObjectNotFound)
	require.ErrorContains(t, err, testKey)
	api.AssertExpectations(t)
}
