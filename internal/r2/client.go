package r2

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"path"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Client struct {
	s3            *s3.Client
	bucket        string
	publicBaseURL string
}

func New(ctx context.Context, endpoint, region, accessKey, secretKey, bucket, publicBaseURL string) (*Client, error) {
	r2Resolver := aws.EndpointResolverWithOptionsFunc(func(service, r string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{URL: endpoint}, nil
	})

	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithEndpointResolverWithOptions(r2Resolver),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("load r2 config: %w", err)
	}

	return &Client{
		s3:            s3.NewFromConfig(cfg),
		bucket:        bucket,
		publicBaseURL: publicBaseURL,
	}, nil
}

func (c *Client) Upload(ctx context.Context, key string, body io.Reader, contentType string) (string, error) {
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("upload to r2: %w", err)
	}

	return c.GetPublicURL(key), nil
}

func (c *Client) GetPublicURL(key string) string {
	u, _ := url.Parse(c.publicBaseURL)
	u.Path = path.Join(u.Path, key)
	return u.String()
}

func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("delete from r2: %w", err)
	}
	return nil
}
