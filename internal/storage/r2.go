package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// R2Config captures the S3-compatible Cloudflare R2 connection settings.
type R2Config struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	PublicBaseURL   string
}

// Complete reports whether enough settings are present to attempt a connection.
func (c R2Config) Complete() bool {
	return strings.TrimSpace(c.Endpoint) != "" &&
		strings.TrimSpace(c.AccessKeyID) != "" &&
		strings.TrimSpace(c.SecretAccessKey) != "" &&
		strings.TrimSpace(c.Bucket) != ""
}

type r2Service struct {
	client        *s3.Client
	bucket        string
	publicBaseURL string
}

// NewR2Service constructs a Cloudflare R2 backed storage service.
func NewR2Service(ctx context.Context, cfg R2Config) (Service, error) {
	if !cfg.Complete() {
		return nil, fmt.Errorf("r2 storage requires endpoint, access key, secret key, and bucket")
	}
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		region = "auto"
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			strings.TrimSpace(cfg.AccessKeyID),
			strings.TrimSpace(cfg.SecretAccessKey),
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("load r2 config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/"))
		o.UsePathStyle = true
	})

	return &r2Service{
		client:        client,
		bucket:        strings.TrimSpace(cfg.Bucket),
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.PublicBaseURL), "/"),
	}, nil
}

// Put uploads the object to the configured bucket.
func (s *r2Service) Put(ctx context.Context, input PutInput) (StoredObject, error) {
	key, err := normaliseKey(input.Key)
	if err != nil {
		return StoredObject{}, err
	}
	contentType := strings.TrimSpace(input.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(input.Body),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return StoredObject{}, fmt.Errorf("upload to r2: %w", err)
	}

	stored := StoredObject{Key: key}
	if input.Visibility == VisibilityPublic {
		if s.publicBaseURL == "" {
			return StoredObject{}, fmt.Errorf("public base url is required for public uploads")
		}
		stored.PublicURL = joinPublicURL(s.publicBaseURL, key)
	}
	return stored, nil
}

// Open streams the object out of the bucket.
func (s *r2Service) Open(ctx context.Context, key string) (OpenedFile, error) {
	clean, err := normaliseKey(key)
	if err != nil {
		return OpenedFile{}, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(clean),
	})
	if err != nil {
		var missing *types.NoSuchKey
		if errors.As(err, &missing) {
			return OpenedFile{}, ErrNotFound
		}
		return OpenedFile{}, fmt.Errorf("get from r2: %w", err)
	}
	opened := OpenedFile{Body: out.Body, ContentType: "application/octet-stream"}
	if out.ContentType != nil && strings.TrimSpace(*out.ContentType) != "" {
		opened.ContentType = *out.ContentType
	}
	if out.ContentLength != nil {
		opened.Size = *out.ContentLength
	}
	return opened, nil
}

// Delete removes the object from the bucket.
func (s *r2Service) Delete(ctx context.Context, key string) error {
	clean, err := normaliseKey(key)
	if err != nil {
		return err
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(clean),
	}); err != nil {
		return fmt.Errorf("delete from r2: %w", err)
	}
	return nil
}

// PublicURL builds the public URL for a key.
func (s *r2Service) PublicURL(key string) string {
	clean, err := normaliseKey(key)
	if err != nil {
		return ""
	}
	return joinPublicURL(s.publicBaseURL, clean)
}
