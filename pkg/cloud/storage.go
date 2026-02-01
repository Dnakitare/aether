package cloud

import (
	"context"
	"io"
	"time"
)

// ObjectStorage provides a cloud-agnostic interface for object storage
// Implementations: S3 (AWS), Cloud Storage (GCP), Blob Storage (Azure)
type ObjectStorage interface {
	// PutObject uploads an object to storage
	PutObject(ctx context.Context, bucket, key string, data io.Reader, metadata map[string]string) error

	// GetObject retrieves an object from storage
	GetObject(ctx context.Context, bucket, key string) (io.ReadCloser, error)

	// DeleteObject deletes an object from storage
	DeleteObject(ctx context.Context, bucket, key string) error

	// ListObjects lists objects with a given prefix
	ListObjects(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)

	// GetObjectMetadata retrieves metadata for an object
	GetObjectMetadata(ctx context.Context, bucket, key string) (*ObjectInfo, error)

	// CopyObject copies an object within or across buckets
	CopyObject(ctx context.Context, sourceBucket, sourceKey, destBucket, destKey string) error

	// GeneratePresignedURL generates a pre-signed URL for temporary access
	GeneratePresignedURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)

	// CreateBucket creates a new bucket/container
	CreateBucket(ctx context.Context, bucket string) error

	// DeleteBucket deletes a bucket/container
	DeleteBucket(ctx context.Context, bucket string) error

	// BucketExists checks if a bucket exists
	BucketExists(ctx context.Context, bucket string) (bool, error)
}

// ObjectInfo contains metadata about an object
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
	ContentType  string
	Metadata     map[string]string
}

// StorageProvider represents different cloud storage providers
type StorageProvider string

const (
	// ProviderAWS represents AWS S3
	ProviderAWS StorageProvider = "aws"

	// ProviderGCP represents Google Cloud Storage
	ProviderGCP StorageProvider = "gcp"

	// ProviderAzure represents Azure Blob Storage
	ProviderAzure StorageProvider = "azure"

	// ProviderLocal represents local filesystem (for testing)
	ProviderLocal StorageProvider = "local"
)

// StorageConfig configures cloud storage
type StorageConfig struct {
	// Provider specifies the storage provider
	Provider StorageProvider

	// Region specifies the cloud region
	Region string

	// Endpoint is a custom endpoint (optional, for S3-compatible storage)
	Endpoint string

	// AccessKey is the access key ID (AWS/S3-compatible)
	AccessKey string

	// SecretKey is the secret access key (AWS/S3-compatible)
	SecretKey string

	// ProjectID is the GCP project ID
	ProjectID string

	// AccountName is the Azure storage account name
	AccountName string

	// AccountKey is the Azure storage account key
	AccountKey string

	// UseSSL enables SSL/TLS connections
	UseSSL bool
}

// NewObjectStorage creates a new object storage client based on the provider
func NewObjectStorage(config StorageConfig) (ObjectStorage, error) {
	switch config.Provider {
	case ProviderAWS:
		// Import cycle prevention: implemented in pkg/cloud/aws/s3.go
		return nil, &CloudError{
			Provider:  string(config.Provider),
			Operation: "NewObjectStorage",
			Message:   "use aws.NewS3Storage() directly",
		}
	case ProviderGCP:
		// Import cycle prevention: implemented in pkg/cloud/gcp/gcs.go
		return nil, &CloudError{
			Provider:  string(config.Provider),
			Operation: "NewObjectStorage",
			Message:   "use gcp.NewGCSStorage() directly",
		}
	case ProviderAzure:
		// Import cycle prevention: implemented in pkg/cloud/azure/blob.go
		return nil, &CloudError{
			Provider:  string(config.Provider),
			Operation: "NewObjectStorage",
			Message:   "use azure.NewBlobStorage() directly",
		}
	case ProviderLocal:
		return newLocalStorage(config)
	default:
		return nil, &CloudError{
			Provider:  string(config.Provider),
			Operation: "NewObjectStorage",
			Message:   "unsupported storage provider",
		}
	}
}

// newLocalStorage creates a local filesystem storage (for testing)
func newLocalStorage(config StorageConfig) (ObjectStorage, error) {
	// Placeholder - implement local filesystem storage for testing
	return nil, &CloudError{
		Provider:  string(config.Provider),
		Operation: "newLocalStorage",
		Message:   "local storage not yet implemented",
	}
}

// CloudError represents a cloud operation error
type CloudError struct {
	Provider  string
	Operation string
	Message   string
	Err       error
}

func (e *CloudError) Error() string {
	if e.Err != nil {
		return e.Provider + ": " + e.Operation + ": " + e.Message + ": " + e.Err.Error()
	}
	return e.Provider + ": " + e.Operation + ": " + e.Message
}

func (e *CloudError) Unwrap() error {
	return e.Err
}
