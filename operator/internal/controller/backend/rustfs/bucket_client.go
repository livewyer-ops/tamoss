package rustfs

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"

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

type DeletePrefixResult struct {
	ObjectsDeleted int64
}

type BucketClient interface {
	Ensure(ctx context.Context, target BucketTarget, creds BucketCredentials) error
	Delete(ctx context.Context, target BucketTarget, creds BucketCredentials) error
	DeletePrefix(ctx context.Context, target BucketTarget, creds BucketCredentials, prefix string) (DeletePrefixResult, error)
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
	if _, err := removeObjects(ctx, client, target.BucketName, minio.ListObjectsOptions{Recursive: true, WithVersions: true}); err != nil {
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

func (S3BucketClient) DeletePrefix(ctx context.Context, target BucketTarget, creds BucketCredentials, prefix string) (DeletePrefixResult, error) {
	client, err := newBucketClient(target, creds)
	if err != nil {
		return DeletePrefixResult{}, err
	}
	result, err := removeObjects(ctx, client, target.BucketName, minio.ListObjectsOptions{
		Prefix:       prefix,
		Recursive:    true,
		WithVersions: true,
	})
	if err != nil {
		return DeletePrefixResult{}, fmt.Errorf("delete objects under prefix %s from bucket %s: %w", prefix, target.BucketName, err)
	}
	return result, nil
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

func removeObjects(ctx context.Context, client *minio.Client, bucketName string, options minio.ListObjectsOptions) (DeletePrefixResult, error) {
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	objects := make(chan minio.ObjectInfo)
	listErr := make(chan error, 1)
	var count atomic.Int64
	go func() {
		defer close(objects)
		for object := range client.ListObjects(listCtx, bucketName, options) {
			if object.Err != nil {
				listErr <- object.Err
				return
			}
			count.Add(1)
			select {
			case objects <- object:
			case <-listCtx.Done():
				return
			}
		}
	}()
	for removal := range client.RemoveObjects(ctx, bucketName, objects, minio.RemoveObjectsOptions{}) {
		if removal.Err != nil {
			cancel()
			return DeletePrefixResult{ObjectsDeleted: count.Load()}, fmt.Errorf("delete object %s from bucket %s: %w", removal.ObjectName, bucketName, removal.Err)
		}
	}
	select {
	case err := <-listErr:
		return DeletePrefixResult{ObjectsDeleted: count.Load()}, fmt.Errorf("list objects in bucket %s: %w", bucketName, err)
	default:
	}
	return DeletePrefixResult{ObjectsDeleted: count.Load()}, nil
}
