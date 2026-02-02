package aws

import (
	"context"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aether-runtime/aether/pkg/cloud"
)

// S3Storage implements ObjectStorage for AWS S3
type S3Storage struct {
	client *s3.Client
	config cloud.StorageConfig
}

// NewS3Storage creates a new S3 storage client
func NewS3Storage(cfg cloud.StorageConfig) (*S3Storage, error) {
	ctx := context.Background()

	// Load AWS config
	awsConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, &cloud.CloudError{
			Provider:  "aws",
			Operation: "LoadDefaultConfig",
			Message:   "failed to load AWS config",
			Err:       err,
		}
	}

	// Create S3 client
	client := s3.NewFromConfig(awsConfig)

	return &S3Storage{
		client: client,
		config: cfg,
	}, nil
}

// PutObject uploads an object to S3
func (s *S3Storage) PutObject(ctx context.Context, bucket, key string, data io.Reader, metadata map[string]string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		Body:     data,
		Metadata: metadata,
	})

	if err != nil {
		return &cloud.CloudError{
			Provider:  "aws",
			Operation: "PutObject",
			Message:   "failed to upload object",
			Err:       err,
		}
	}

	return nil
}

// GetObject retrieves an object from S3
func (s *S3Storage) GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return nil, &cloud.CloudError{
			Provider:  "aws",
			Operation: "GetObject",
			Message:   "failed to download object",
			Err:       err,
		}
	}

	return result.Body, nil
}

// DeleteObject deletes an object from S3
func (s *S3Storage) DeleteObject(ctx context.Context, bucket, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return &cloud.CloudError{
			Provider:  "aws",
			Operation: "DeleteObject",
			Message:   "failed to delete object",
			Err:       err,
		}
	}

	return nil
}

// ListObjects lists objects with a given prefix
func (s *S3Storage) ListObjects(ctx context.Context, bucket, prefix string) ([]cloud.ObjectInfo, error) {
	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})

	if err != nil {
		return nil, &cloud.CloudError{
			Provider:  "aws",
			Operation: "ListObjects",
			Message:   "failed to list objects",
			Err:       err,
		}
	}

	objects := make([]cloud.ObjectInfo, 0, len(result.Contents))
	for _, obj := range result.Contents {
		objects = append(objects, cloud.ObjectInfo{
			Key:          aws.ToString(obj.Key),
			Size:         aws.ToInt64(obj.Size),
			LastModified: aws.ToTime(obj.LastModified),
			ETag:         aws.ToString(obj.ETag),
		})
	}

	return objects, nil
}

// GetObjectMetadata retrieves metadata for an object
func (s *S3Storage) GetObjectMetadata(ctx context.Context, bucket, key string) (*cloud.ObjectInfo, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})

	if err != nil {
		return nil, &cloud.CloudError{
			Provider:  "aws",
			Operation: "GetObjectMetadata",
			Message:   "failed to get object metadata",
			Err:       err,
		}
	}

	return &cloud.ObjectInfo{
		Key:          key,
		Size:         aws.ToInt64(result.ContentLength),
		LastModified: aws.ToTime(result.LastModified),
		ETag:         aws.ToString(result.ETag),
		ContentType:  aws.ToString(result.ContentType),
		Metadata:     result.Metadata,
	}, nil
}

// CopyObject copies an object within or across buckets
func (s *S3Storage) CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey string) error {
	copySource := sourceBucket + "/" + sourceKey

	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(destBucket),
		Key:        aws.String(destKey),
		CopySource: aws.String(copySource),
	})

	if err != nil {
		return &cloud.CloudError{
			Provider:  "aws",
			Operation: "CopyObject",
			Message:   "failed to copy object",
			Err:       err,
		}
	}

	return nil
}

// GeneratePresignedURL generates a pre-signed URL for temporary access
func (s *S3Storage) GeneratePresignedURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(s.client)

	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))

	if err != nil {
		return "", &cloud.CloudError{
			Provider:  "aws",
			Operation: "GeneratePresignedURL",
			Message:   "failed to generate presigned URL",
			Err:       err,
		}
	}

	return result.URL, nil
}

// CreateBucket creates a new S3 bucket
func (s *S3Storage) CreateBucket(ctx context.Context, bucket string) error {
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return &cloud.CloudError{
			Provider:  "aws",
			Operation: "CreateBucket",
			Message:   "failed to create bucket",
			Err:       err,
		}
	}

	return nil
}

// DeleteBucket deletes an S3 bucket
func (s *S3Storage) DeleteBucket(ctx context.Context, bucket string) error {
	_, err := s.client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		return &cloud.CloudError{
			Provider:  "aws",
			Operation: "DeleteBucket",
			Message:   "failed to delete bucket",
			Err:       err,
		}
	}

	return nil
}

// BucketExists checks if an S3 bucket exists
func (s *S3Storage) BucketExists(ctx context.Context, bucket string) (bool, error) {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})

	if err != nil {
		// Check if error is "not found"
		return false, nil
	}

	return true, nil
}
