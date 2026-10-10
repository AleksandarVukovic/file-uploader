package objectstore

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/aleksandarv/file-uploader/common/aws/s3"
	"github.com/aleksandarv/file-uploader/file-service/internal/service/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type s3Client struct {
	s3         s3.API
	bucketName string
}

func NewS3(bucketName string, api s3.API) storage.ObjectStore {
	return &s3Client{s3: api, bucketName: bucketName}
}

func (s *s3Client) Put(ctx context.Context, in storage.PutInput) error {
	checksumSHA256 := base64.StdEncoding.EncodeToString(in.ChecksumSHA256)

	_, err := s.s3.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:         aws.String(s.bucketName),
		Key:            aws.String(in.Key),
		Body:           in.Body,
		ContentLength:  aws.Int64(in.Size),
		ContentType:    aws.String(in.ContentType),
		ChecksumSHA256: aws.String(checksumSHA256),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.ErrorCode() {
			case "BadDigest":
				return fmt.Errorf("put object %q: %w", in.Key, storage.ErrChecksumMismatch)
			case "IncompleteBody":
				return fmt.Errorf("put object %q: %w", in.Key, storage.ErrIncompleteBody)
			}
		}
		return fmt.Errorf("put object %q: %w", in.Key, err)
	}
	return nil
}
