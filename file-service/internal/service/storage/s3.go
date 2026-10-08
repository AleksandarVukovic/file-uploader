package storage

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"strconv"

	"github.com/aleksandarv/file-uploader/common/aws/s3"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type s3Service struct {
	s3         s3.API
	bucketName string
}

func NewS3(bucketName string, s3Client s3.API) Service {
	return &s3Service{s3: s3Client, bucketName: bucketName}
}

func (s *s3Service) Upload(ctx context.Context, in UploadInput) (UploadResult, error) {
	checksum, err := hex.DecodeString(in.Checksum)
	if err != nil {
		return UploadResult{}, ErrInvalidChecksum
	}

	key := strconv.FormatInt(in.UserID, 10) + "/" + in.UUID
	checksumSHA256 := base64.StdEncoding.EncodeToString(checksum)

	_, err = s.s3.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:         aws.String(s.bucketName),
		Key:            aws.String(key),
		Body:           in.Body,
		ContentLength:  aws.Int64(in.Size),
		ContentType:    aws.String(in.ContentType),
		ChecksumSHA256: aws.String(checksumSHA256),
	})
	if err != nil {
		return UploadResult{}, err
	}
	return UploadResult{
		UUID:        in.UUID,
		Filename:    in.Filename,
		ContentType: in.ContentType,
		Size:        in.Size,
	}, nil
}
