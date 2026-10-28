// Package s3storage 提供基于 aws-sdk-go-v2 的 S3 兼容对象存储。
// 一个驱动覆盖所有 S3 兼容服务商（MinIO/R2/AWS S3/B2/Spaces/Wasabi/OSS/COS/Kodo/OBS 等），
// 仅通过 Endpoint/Region 配置区分。
package s3storage

import (
	"context"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/Mythicove/shared/storage"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// Options 是 S3 存储配置。
type Options struct {
	Endpoint  string // S3 兼容端点（含 scheme），空 = 官方 AWS
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

// Storage 是 S3 兼容存储。
type Storage struct {
	client    *s3.Client
	presigner *s3.PresignClient
	bucket    string
	baseURL   string
}

var _ storage.Storage = (*Storage)(nil)

// New 构造 S3 存储。
func New(ctx context.Context, opts Options, baseURL string) (*Storage, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(opts.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(opts.AccessKey, opts.SecretKey, "")),
	)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if opts.Endpoint != "" {
			o.BaseEndpoint = aws.String(opts.Endpoint)
			o.UsePathStyle = true
		}
	})
	return &Storage{
		client:    client,
		presigner: s3.NewPresignClient(client),
		bucket:    opts.Bucket,
		baseURL:   baseURL,
	}, nil
}

func (s *Storage) Put(ctx context.Context, o storage.Object) (string, error) {
	in := &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &o.Key,
		Body:        o.Body,
		ContentType: &o.ContentType,
	}
	if o.Size > 0 {
		in.ContentLength = aws.Int64(o.Size)
	}
	_, err := s.client.PutObject(ctx, in)
	return o.Key, err
}

func (s *Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	return err
}

// PresignGet 返回直出 URL；baseURL 为站点基址时拼 /textures/ 前缀（与 filestorage 契约一致，
// 对应网关 /textures/* 路由），留空时生成预签名 URL（私有桶）。
func (s *Storage) PresignGet(ctx context.Context, key string) (*url.URL, error) {
	if s.baseURL != "" {
		return url.Parse(strings.TrimRight(s.baseURL, "/") + "/textures/" + key)
	}
	req, err := s.presigner.PresignGetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key},
		s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return nil, err
	}
	return url.Parse(req.URL)
}
