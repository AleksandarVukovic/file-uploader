package objectstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/aleksandarv/file-uploader/file-processing-service/internal/service/processor"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type s3API interface {
	GetObject(ctx context.Context, params *awss3.GetObjectInput, optFns ...func(*awss3.Options)) (*awss3.GetObjectOutput, error)
}

type s3Client struct {
	s3             s3API
	allowedBuckets []string
}

func NewS3(allowedBuckets []string, api s3API) processor.ObjectStore {
	return &s3Client{s3: api, allowedBuckets: slices.Clone(allowedBuckets)}
}

func (s *s3Client) Get(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	if !slices.Contains(s.allowedBuckets, bucket) {
		return nil, fmt.Errorf("get object %q: bucket %q is not one of %v: %w", key, bucket, s.allowedBuckets, processor.ErrBucketNotAllowed)
	}

	out, err := s.s3.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("get object %q: %w", key, processor.ErrObjectNotFound)
		}
		return nil, fmt.Errorf("get object %q: %w", key, err)
	}
	return out.Body, nil
}
