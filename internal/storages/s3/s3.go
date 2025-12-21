package s3

import (
	"context"
	"fmt"
	"io"
	"sort"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/shyim/docker-backup/internal/storage"
)

func init() {
	storage.Register(&S3StorageType{})
}

// S3StorageType is the factory for S3 storage
type S3StorageType struct{}

// Name returns the storage type identifier
func (t *S3StorageType) Name() string {
	return "s3"
}

// Create instantiates a new S3 storage from options
func (t *S3StorageType) Create(poolName string, options map[string]string) (storage.Storage, error) {
	bucket, ok := options["bucket"]
	if !ok || bucket == "" {
		return nil, fmt.Errorf("S3 storage requires 'bucket' option")
	}

	region := options["region"]
	if region == "" {
		region = "us-east-1"
	}

	endpoint := options["endpoint"]
	accessKey := options["access-key"]
	secretKey := options["secret-key"]
	pathStyle := options["path-style"] == "true"

	prefix := options["prefix"]

	ctx := context.Background()

	// Build AWS config
	var cfgOpts []func(*config.LoadOptions) error
	cfgOpts = append(cfgOpts, config.WithRegion(region))

	// Use static credentials if provided
	if accessKey != "" && secretKey != "" {
		cfgOpts = append(cfgOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(ctx, cfgOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Build S3 client options
	var s3Opts []func(*s3.Options)

	if endpoint != "" {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}

	if pathStyle {
		s3Opts = append(s3Opts, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(cfg, s3Opts...)
	uploader := manager.NewUploader(client)

	return &S3Storage{
		client:   client,
		uploader: uploader,
		bucket:   bucket,
		prefix:   prefix,
		poolName: poolName,
	}, nil
}

// S3Storage implements Storage for S3-compatible backends
type S3Storage struct {
	client   *s3.Client
	uploader *manager.Uploader
	bucket   string
	prefix   string
	poolName string
}

// Store saves backup data to S3 using multipart upload for streaming
func (s *S3Storage) Store(ctx context.Context, key string, reader io.Reader) error {
	fullKey := s.fullKey(key)

	_, err := s.uploader.Upload(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(fullKey),
		Body:        reader,
		ContentType: aws.String("application/gzip"),
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// List returns all backups matching the prefix
func (s *S3Storage) List(ctx context.Context, prefix string) ([]storage.BackupFile, error) {
	fullPrefix := s.fullKey(prefix)

	var files []storage.BackupFile
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(fullPrefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			relKey := *obj.Key
			if s.prefix != "" {
				relKey = relKey[len(s.prefix)+1:]
			}

			files = append(files, storage.BackupFile{
				Key:          relKey,
				Size:         *obj.Size,
				LastModified: *obj.LastModified,
			})
		}
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].LastModified.After(files[j].LastModified)
	})

	return files, nil
}

// Delete removes a backup from S3
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	fullKey := s.fullKey(key)

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		return fmt.Errorf("failed to delete from S3: %w", err)
	}

	return nil
}

// Get retrieves a backup from S3
func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	fullKey := s.fullKey(key)

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(fullKey),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get from S3: %w", err)
	}

	return result.Body, nil
}

// fullKey returns the full S3 key including any prefix
func (s *S3Storage) fullKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}
