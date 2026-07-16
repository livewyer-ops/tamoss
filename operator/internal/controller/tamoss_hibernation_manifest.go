package controller

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tamossv1alpha1 "github.com/livewyer-ops/tamoss/operator/api/v1alpha1"
	schemabundle "github.com/livewyer-ops/tamoss/operator/internal/schema"
)

const (
	hibernationManifestVersion          = "v1"
	hibernationManifestChecksumMetadata = "tamoss-checksum"
	hibernationManifestMaximumBytes     = 1 << 20
)

var (
	errHibernationManifestChecksumMismatch = errors.New("hibernation manifest checksum mismatch")
	errHibernationManifestInvalid          = errors.New("hibernation manifest invalid")
)

type HibernationManifestWriter interface {
	Write(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec, key string, manifest hibernationManifest) (string, error)
}

type HibernationManifestReader interface {
	Read(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec, key string) (hibernationManifest, string, error)
}

type S3HibernationManifestWriter struct {
	Client client.Client
}

type S3HibernationManifestReader struct {
	Client client.Client
}

type hibernationManifest struct {
	ManifestVersion string                            `json:"manifestVersion"`
	CreatedAt       string                            `json:"createdAt"`
	Driver          string                            `json:"driver"`
	SourceTamoss    hibernationManifestTamoss         `json:"sourceTamoss"`
	Schema          hibernationManifestSchema         `json:"schema"`
	Database        hibernationManifestDatabase       `json:"database"`
	Artifact        hibernationManifestArtifact       `json:"artifact"`
	CNPG            hibernationManifestCNPG           `json:"cnpg,omitempty"`
	StorageBackend  hibernationManifestStorageBackend `json:"storageBackend"`
}

type hibernationManifestTamoss struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	UID       string `json:"uid,omitempty"`
}

type hibernationManifestSchema struct {
	Version      string `json:"version,omitempty"`
	TAMSAPI      string `json:"tamsAPI,omitempty"`
	Operator     string `json:"operator,omitempty"`
	ManifestKind string `json:"manifestKind"`
}

type hibernationManifestDatabase struct {
	Provider string `json:"provider"`
	Cluster  string `json:"cluster,omitempty"`
}

type hibernationManifestArtifact struct {
	ManifestKey string `json:"manifestKey"`
	ManifestURI string `json:"manifestURI,omitempty"`
}

type hibernationManifestCNPG struct {
	BackupName      string `json:"backupName"`
	BackupID        string `json:"backupID,omitempty"`
	DestinationPath string `json:"destinationPath,omitempty"`
	ServerName      string `json:"serverName,omitempty"`
	Phase           string `json:"phase,omitempty"`
}

type hibernationManifestStorageBackend struct {
	Name        string `json:"name"`
	Bucket      string `json:"bucket"`
	EndpointURL string `json:"endpointURL"`
	Region      string `json:"region,omitempty"`
}

func buildHibernationManifest(tamoss *tamossv1alpha1.Tamoss, storageBackend *tamossv1alpha1.StorageBackend, spec tamossv1alpha1.StorageBackendSpec, artifact tamossv1alpha1.HibernationArtifactStatus, serverName string) hibernationManifest {
	return hibernationManifest{
		ManifestVersion: hibernationManifestVersion,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		Driver:          artifact.Driver,
		SourceTamoss: hibernationManifestTamoss{
			Name:      tamoss.Name,
			Namespace: tamoss.Namespace,
			UID:       string(tamoss.UID),
		},
		Schema: hibernationManifestSchema{
			Version:      tamoss.Status.SchemaVersion,
			TAMSAPI:      schemabundle.SupportedTAMSAPIVersion,
			Operator:     schemabundle.SchemaVersion,
			ManifestKind: "TamossHibernate",
		},
		Database: hibernationManifestDatabase{
			Provider: string(tamoss.Spec.Backends.DB.Provider()),
			Cluster:  serverName,
		},
		Artifact: hibernationManifestArtifact{
			ManifestKey: artifact.ManifestKey,
			ManifestURI: artifact.ManifestURI,
		},
		CNPG: hibernationManifestCNPG{
			BackupName:      artifact.CNPGBackup.Name,
			BackupID:        artifact.CNPGBackup.BackupID,
			DestinationPath: artifact.CNPGBackup.DestinationPath,
			ServerName:      serverName,
			Phase:           artifact.CNPGBackup.Phase,
		},
		StorageBackend: hibernationManifestStorageBackend{
			Name:        storageBackend.Name,
			Bucket:      spec.BucketName,
			EndpointURL: spec.Endpoint.Default.URL,
			Region:      spec.Region,
		},
	}
}

func (w S3HibernationManifestWriter) Write(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec, key string, manifest hibernationManifest) (string, error) {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal hibernation manifest: %w", err)
	}
	checksum := hibernationManifestChecksum(data)

	creds, err := w.storageBackendCredentials(ctx, namespace, spec)
	if err != nil {
		return "", err
	}
	endpoint, secure, err := hibernationS3Endpoint(spec.Endpoint.Default.URL)
	if err != nil {
		return "", err
	}
	s3Client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure:       secure,
		Region:       spec.Region,
		BucketLookup: minio.BucketLookupPath,
		MaxRetries:   3,
	})
	if err != nil {
		return "", fmt.Errorf("create S3 client for hibernation manifest: %w", err)
	}
	_, err = s3Client.PutObject(ctx, spec.BucketName, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType: "application/json",
		UserMetadata: map[string]string{
			hibernationManifestChecksumMetadata: checksum,
		},
	})
	if err != nil {
		return "", fmt.Errorf("upload hibernation manifest %s/%s: %w", spec.BucketName, key, err)
	}
	return checksum, nil
}

func (r S3HibernationManifestReader) Read(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec, key string) (hibernationManifest, string, error) {
	creds, err := storageBackendCredentials(ctx, r.Client, namespace, spec)
	if err != nil {
		return hibernationManifest{}, "", err
	}
	endpoint, secure, err := hibernationS3Endpoint(spec.Endpoint.Default.URL)
	if err != nil {
		return hibernationManifest{}, "", err
	}
	s3Client, err := minio.New(endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, ""),
		Secure:       secure,
		Region:       spec.Region,
		BucketLookup: minio.BucketLookupPath,
		MaxRetries:   3,
	})
	if err != nil {
		return hibernationManifest{}, "", fmt.Errorf("create S3 client for hibernation manifest: %w", err)
	}
	object, err := s3Client.GetObject(ctx, spec.BucketName, key, minio.GetObjectOptions{})
	if err != nil {
		return hibernationManifest{}, "", fmt.Errorf("open hibernation manifest %s/%s: %w", spec.BucketName, key, err)
	}
	defer func() {
		_ = object.Close()
	}()
	info, err := object.Stat()
	if err != nil {
		return hibernationManifest{}, "", fmt.Errorf("stat hibernation manifest %s/%s: %w", spec.BucketName, key, err)
	}
	if info.Size > hibernationManifestMaximumBytes {
		return hibernationManifest{}, "", fmt.Errorf("%w: hibernation manifest %s/%s is %d bytes; maximum is %d", errHibernationManifestInvalid, spec.BucketName, key, info.Size, hibernationManifestMaximumBytes)
	}
	data, err := io.ReadAll(io.LimitReader(object, hibernationManifestMaximumBytes+1))
	if err != nil {
		return hibernationManifest{}, "", fmt.Errorf("read hibernation manifest %s/%s: %w", spec.BucketName, key, err)
	}
	if len(data) > hibernationManifestMaximumBytes {
		return hibernationManifest{}, "", fmt.Errorf("%w: hibernation manifest %s/%s exceeds %d bytes", errHibernationManifestInvalid, spec.BucketName, key, hibernationManifestMaximumBytes)
	}
	checksum := hibernationManifestChecksum(data)
	if metadataChecksum := hibernationManifestMetadataChecksum(info.UserMetadata); metadataChecksum != "" && metadataChecksum != checksum {
		return hibernationManifest{}, "", fmt.Errorf("%w: metadata %s, computed %s", errHibernationManifestChecksumMismatch, metadataChecksum, checksum)
	}
	var manifest hibernationManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return hibernationManifest{}, "", fmt.Errorf("%w: parse hibernation manifest %s/%s: %v", errHibernationManifestInvalid, spec.BucketName, key, err)
	}
	if manifest.ManifestVersion != hibernationManifestVersion {
		return hibernationManifest{}, "", fmt.Errorf("%w: unsupported hibernation manifest version %q", errHibernationManifestInvalid, manifest.ManifestVersion)
	}
	return manifest, checksum, nil
}

// isPermanentHibernationManifestReadError separates manifest read failures the
// resume controller must not retry (corrupt or absent artifacts) from
// transient transport failures that resolve on their own.
func isPermanentHibernationManifestReadError(err error) bool {
	if errors.Is(err, errHibernationManifestChecksumMismatch) || errors.Is(err, errHibernationManifestInvalid) {
		return true
	}
	var response minio.ErrorResponse
	if errors.As(err, &response) {
		switch response.Code {
		case "NoSuchKey", "NoSuchBucket":
			return true
		}
	}
	return false
}

func hibernationManifestChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}

func hibernationManifestMetadataChecksum(metadata map[string]string) string {
	for key, value := range metadata {
		normalized := strings.ToLower(strings.TrimPrefix(strings.ToLower(key), "x-amz-meta-"))
		if normalized == hibernationManifestChecksumMetadata {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (w S3HibernationManifestWriter) storageBackendCredentials(ctx context.Context, namespace string, spec tamossv1alpha1.StorageBackendSpec) (storageBackendS3Credentials, error) {
	return storageBackendCredentials(ctx, w.Client, namespace, spec)
}

type storageBackendS3Credentials struct {
	AccessKey string
	SecretKey string
}

func storageBackendCredentials(ctx context.Context, c client.Client, namespace string, spec tamossv1alpha1.StorageBackendSpec) (storageBackendS3Credentials, error) {
	secret := &corev1.Secret{}
	if err := c.Get(ctx, types.NamespacedName{Name: spec.Credentials.ExistingSecret, Namespace: namespace}, secret); err != nil {
		return storageBackendS3Credentials{}, err
	}
	return storageBackendS3Credentials{
		AccessKey: string(secret.Data[storageBackendAccessKey(spec)]),
		SecretKey: string(secret.Data[storageBackendSecretKey(spec)]),
	}, nil
}

func hibernationS3Endpoint(raw string) (string, bool, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", false, fmt.Errorf("parse hibernation S3 endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false, fmt.Errorf("hibernation S3 endpoint must use http or https")
	}
	if parsed.Host == "" {
		return "", false, fmt.Errorf("hibernation S3 endpoint host is required")
	}
	return parsed.Host, parsed.Scheme == "https", nil
}
