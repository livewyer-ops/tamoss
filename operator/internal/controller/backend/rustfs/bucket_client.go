package rustfs

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/cors"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type BucketTarget struct {
	EndpointURL string
	BucketName  string
	Region      string
	CORSOrigins []string
}

type BucketCredentials struct {
	AccessKey string
	SecretKey string
}

type BucketClient interface {
	Ensure(ctx context.Context, target BucketTarget, creds BucketCredentials) error
	Delete(ctx context.Context, target BucketTarget, creds BucketCredentials) error
}

type S3BucketClient struct{}

func (S3BucketClient) Ensure(ctx context.Context, target BucketTarget, creds BucketCredentials) error {
	client, err := newBucketClient(target, creds)
	if err != nil {
		return err
	}
	exists, err := client.BucketExists(ctx, target.BucketName)
	if err != nil {
		return fmt.Errorf("check bucket %s: %w", target.BucketName, err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, target.BucketName, minio.MakeBucketOptions{Region: target.Region}); err != nil {
			return fmt.Errorf("create bucket %s: %w", target.BucketName, err)
		}
	}
	if len(target.CORSOrigins) > 0 {
		if err := client.SetBucketCors(ctx, target.BucketName, corsConfig(target.CORSOrigins)); err != nil {
			return fmt.Errorf("configure bucket CORS for %s: %w", target.BucketName, err)
		}
	}
	return nil
}

func (S3BucketClient) Delete(ctx context.Context, target BucketTarget, creds BucketCredentials) error {
	client, err := newBucketClient(target, creds)
	if err != nil {
		return err
	}
	exists, err := client.BucketExists(ctx, target.BucketName)
	if err != nil {
		return fmt.Errorf("check bucket %s before deletion: %w", target.BucketName, err)
	}
	if !exists {
		return nil
	}
	if err := emptyBucket(ctx, client, target.BucketName); err != nil {
		return err
	}
	if err := client.RemoveBucket(ctx, target.BucketName); err != nil {
		response := minio.ToErrorResponse(err)
		if response.Code == "NoSuchBucket" || response.Code == "NoSuchBucketPolicy" {
			return nil
		}
		return fmt.Errorf("delete bucket %s: %w", target.BucketName, err)
	}
	return nil
}

func newBucketClient(target BucketTarget, creds BucketCredentials) (*minio.Client, error) {
	if strings.TrimSpace(creds.AccessKey) == "" || strings.TrimSpace(creds.SecretKey) == "" {
		return nil, fmt.Errorf("bucket credentials are incomplete")
	}
	endpoint, secure, err := minioEndpoint(target.EndpointURL)
	if err != nil {
		return nil, err
	}
	return minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure:       secure,
		Region:       target.Region,
		BucketLookup: minio.BucketLookupPath,
		MaxRetries:   3,
	})
}

func minioEndpoint(raw string) (string, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false, fmt.Errorf("parse bucket endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false, fmt.Errorf("bucket endpoint must use http or https")
	}
	if parsed.Host == "" {
		return "", false, fmt.Errorf("bucket endpoint host is required")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}

func corsConfig(origins []string) *cors.Config {
	return cors.NewConfig([]cors.Rule{{
		AllowedOrigin: origins,
		AllowedMethod: []string{"GET", "HEAD", "PUT", "POST"},
		AllowedHeader: []string{"*"},
		ExposeHeader:  []string{"ETag"},
	}})
}

func emptyBucket(ctx context.Context, client *minio.Client, bucketName string) error {
	objects := client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{Recursive: true, WithVersions: true})
	for removal := range client.RemoveObjects(ctx, bucketName, objects, minio.RemoveObjectsOptions{}) {
		if removal.Err != nil {
			return fmt.Errorf("delete object %s from bucket %s: %w", removal.ObjectName, bucketName, removal.Err)
		}
	}
	return nil
}
