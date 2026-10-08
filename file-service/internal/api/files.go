package api

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"

	"github.com/aleksandarv/file-uploader/common/aws/s3"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/file-service/gen/files"
	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

type filesHandler struct {
	s3         s3.API
	bucketName string
}

func NewFilesHandler(bucketName string, s3Client s3.API) files.Service {
	return &filesHandler{
		s3:         s3Client,
		bucketName: bucketName,
	}
}

func (h *filesHandler) Upload(ctx context.Context, p *files.UploadPayload, body io.ReadCloser) error {
	log := logger.FromCtx(ctx)
	defer body.Close()

	checksumBytes, err := hex.DecodeString(p.Checksum)
	if err != nil {
		log.Error("upload rejected: malformed checksum", "filename", p.Filename, "checksum", p.Checksum)
		return files.MakeBadRequest(errors.New("Checksum must be a hex-encoded SHA-256 digest"))
	}

	checksumB64 := base64.StdEncoding.EncodeToString(checksumBytes)
	po, err := h.s3.PutObject(ctx, &awss3.PutObjectInput{
		Bucket:         aws.String(h.bucketName),
		Key:            aws.String(p.Filename),
		Body:           body,
		ContentLength:  aws.Int64(p.Size),
		ContentType:    aws.String(p.ContentType),
		ChecksumSHA256: aws.String(checksumB64),
	})
	if err != nil {
		log.Error("failed to upload file to S3", "err", err, "filename", p.Filename)
		return files.MakeInternalError(errors.New("Error while storing file"))
	}

	log.Info("file uploaded to S3", "filename", p.Filename, "checksum", po.ChecksumSHA256, "size", po.Size)
	return nil
}
