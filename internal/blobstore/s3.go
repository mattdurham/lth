// NOTE: Any changes to this file must be reflected in the corresponding SPECS.md or NOTES.md.

package blobstore

import (
	"context"
	"fmt"
	"io"
	"io/fs"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Store implements BlobStore using an S3-compatible object store via minio-go.
type S3Store struct {
	client *minio.Client
	bucket string
}

// NewS3Store creates an S3Store from the given config.
// Returns an error if the bucket is not accessible.
func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	creds := credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, "")
	opts := &minio.Options{
		Creds:  creds,
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	}
	client, err := minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}
	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket %q: %w", cfg.Bucket, err)
	}
	if !exists {
		return nil, fmt.Errorf("bucket %q does not exist or is not accessible", cfg.Bucket)
	}
	return &S3Store{client: client, bucket: cfg.Bucket}, nil
}

func (s *S3Store) Put(ctx context.Context, key string, r io.Reader) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, -1, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("put object %q: %w", key, err)
	}
	return nil
}

func (s *S3Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return nil, fmt.Errorf("key %q: %w", key, fs.ErrNotExist)
		}
		return nil, fmt.Errorf("get object %q: %w", key, err)
	}
	// Trigger an actual read to detect NoSuchKey at this point.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return nil, fmt.Errorf("key %q: %w", key, fs.ErrNotExist)
		}
		return nil, fmt.Errorf("stat object %q: %w", key, err)
	}
	return obj, nil
}

func (s *S3Store) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.Code == "NoSuchKey" {
		return false, nil
	}
	return false, fmt.Errorf("stat object %q: %w", key, err)
}

func (s *S3Store) Delete(ctx context.Context, key string) error {
	err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return nil
		}
		return fmt.Errorf("remove object %q: %w", key, err)
	}
	return nil
}

func (s *S3Store) List(ctx context.Context, prefix string) ([]BlobObject, error) {
	var results []BlobObject
	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}
	for obj := range s.client.ListObjects(ctx, s.bucket, opts) {
		if obj.Err != nil {
			return nil, fmt.Errorf("list objects: %w", obj.Err)
		}
		results = append(results, BlobObject{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: obj.LastModified,
		})
	}
	if results == nil {
		results = []BlobObject{}
	}
	return results, nil
}
