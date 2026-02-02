# Cloud Abstraction Layer

Cloud-agnostic interfaces for multi-cloud deployments.

## Overview

This package provides unified interfaces for common cloud services across AWS, GCP, and Azure. This allows Aether to be deployed on any cloud provider without code changes.

## Supported Providers

- **AWS** - Amazon Web Services
- **GCP** - Google Cloud Platform
- **Azure** - Microsoft Azure (placeholder)
- **Local** - Local filesystem (for testing)

## Abstractions

### Object Storage

Unified interface for S3, Cloud Storage, and Azure Blob Storage.

**AWS S3:**
```go
import "github.com/aether-runtime/aether/pkg/cloud/aws"

config := cloud.StorageConfig{
    Provider: cloud.ProviderAWS,
    Region:   "us-east-1",
}

storage, err := aws.NewS3Storage(config)
if err != nil {
    log.Fatal(err)
}

// Upload object
err = storage.PutObject(ctx, "my-bucket", "path/to/file.txt",
    bytes.NewReader(data), nil)

// Download object
reader, err := storage.GetObject(ctx, "my-bucket", "path/to/file.txt")
if err != nil {
    log.Fatal(err)
}
defer reader.Close()

// List objects
objects, err := storage.ListObjects(ctx, "my-bucket", "path/")
for _, obj := range objects {
    fmt.Printf("%s (%d bytes)\n", obj.Key, obj.Size)
}
```

**GCP Cloud Storage:**
```go
import "github.com/aether-runtime/aether/pkg/cloud/gcp"

config := cloud.StorageConfig{
    Provider:  cloud.ProviderGCP,
    ProjectID: "my-project",
}

storage, err := gcp.NewGCSStorage(config)
// Use same API as AWS S3
```

### Block Storage (Future)

Interface for EBS, Persistent Disks, and Azure Disks.

### Networking (Future)

Interfaces for VPC, VNet, security groups, and load balancers.

### Secrets Management (Future)

Interface for Secrets Manager, Secret Manager, and Key Vault.

## Design Principles

1. **Provider Agnostic** - Same code works across all clouds
2. **Minimal Dependencies** - Only import what you use
3. **Error Handling** - Consistent error types across providers
4. **Performance** - Direct provider SDKs, no abstraction overhead

## Implementation Status

| Feature | AWS | GCP | Azure |
|---------|-----|-----|-------|
| Object Storage | ✅ | 🔄 | ⏳ |
| Block Storage | ⏳ | ⏳ | ⏳ |
| Networking | ⏳ | ⏳ | ⏳ |
| Secrets | ⏳ | ⏳ | ⏳ |
| Databases | N/A | N/A | N/A |

Legend: ✅ Complete, 🔄 In Progress, ⏳ Planned, N/A Not Applicable

## Error Handling

All cloud operations return `*cloud.CloudError` on failure:

```go
obj, err := storage.GetObject(ctx, bucket, key)
if err != nil {
    var cloudErr *cloud.CloudError
    if errors.As(err, &cloudErr) {
        log.Printf("Cloud error: provider=%s operation=%s message=%s",
            cloudErr.Provider, cloudErr.Operation, cloudErr.Message)
    }
    return err
}
```

## Testing

Use the local provider for testing:

```go
config := cloud.StorageConfig{
    Provider: cloud.ProviderLocal,
}

storage, err := cloud.NewObjectStorage(config)
// Uses local filesystem instead of cloud storage
```

## Migration Guide

### From AWS SDK Directly

**Before:**
```go
import "github.com/aws/aws-sdk-go-v2/service/s3"

s3Client := s3.NewFromConfig(awsConfig)
_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
    Bucket: aws.String(bucket),
    Key:    aws.String(key),
    Body:   data,
})
```

**After:**
```go
import "github.com/aether-runtime/aether/pkg/cloud/aws"

storage, _ := aws.NewS3Storage(config)
err := storage.PutObject(ctx, bucket, key, data, nil)
```

### Multi-Cloud Support

Use a factory pattern to select provider at runtime:

```go
func newStorage(provider string) (cloud.ObjectStorage, error) {
    config := cloud.StorageConfig{
        Provider: cloud.StorageProvider(provider),
        Region:   os.Getenv("CLOUD_REGION"),
    }

    switch config.Provider {
    case cloud.ProviderAWS:
        return aws.NewS3Storage(config)
    case cloud.ProviderGCP:
        return gcp.NewGCSStorage(config)
    default:
        return nil, fmt.Errorf("unsupported provider: %s", provider)
    }
}

// Use from config
storage, err := newStorage(os.Getenv("CLOUD_PROVIDER"))
```

## Contributing

When adding new abstractions:

1. Define the interface in `pkg/cloud/`
2. Implement for AWS in `pkg/cloud/aws/`
3. Implement for GCP in `pkg/cloud/gcp/`
4. Add tests for each implementation
5. Update this README

## References

- [AWS SDK for Go v2](https://github.com/aws/aws-sdk-go-v2)
- [Google Cloud Go SDK](https://github.com/googleapis/google-cloud-go)
- [Azure SDK for Go](https://github.com/Azure/azure-sdk-for-go)
