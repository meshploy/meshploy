package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"time"

	appk8s "github.com/meshploy/apps/api/internal/k8s"
	db "github.com/meshploy/packages/db"
	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/google/uuid"
)

// ─── S3 helpers ───────────────────────────────────────────────────────────────

func newS3Client(sto db.StorageIntegration) (*minio.Client, error) {
	endpoint := sto.Endpoint
	secure := true
	if endpoint == "" {
		endpoint = "s3.amazonaws.com"
	} else if after, ok := strings.CutPrefix(endpoint, "http://"); ok {
		endpoint = after
		secure = false
	} else {
		endpoint, _ = strings.CutPrefix(endpoint, "https://")
	}
	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(string(sto.AccessKeyID), string(sto.SecretAccessKey), ""),
		Secure: secure,
		Region: sto.Region,
	})
}

func uploadToS3(ctx context.Context, client *minio.Client, bucket, key string, r io.Reader) error {
	// -1 size triggers streaming multipart upload. PartSize is capped at 16 MB
	// (down from minio's 128 MB default) so concurrent uploads don't exhaust
	// the API container's memory.
	_, err := client.PutObject(ctx, bucket, key, r, -1, minio.PutObjectOptions{
		ContentType: "application/gzip",
		PartSize:    16 * 1024 * 1024,
	})
	return err
}

func purgeOldBackups(ctx context.Context, client *minio.Client, bucket, listPrefix string, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	for obj := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Prefix:    listPrefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			log.Printf("backup retention: list: %v", obj.Err)
			continue
		}
		if obj.LastModified.Before(cutoff) {
			if err := client.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{}); err != nil {
				log.Printf("backup retention: remove %s: %v", obj.Key, err)
			} else {
				log.Printf("backup retention: removed %s (age %s)", obj.Key, time.Since(obj.LastModified).Round(time.Hour))
			}
		}
	}
}

// backupKey builds the S3 object key for a backup file.
// Format: {prefix}/{name}/{timestamp}{ext}   (prefix omitted when empty)
func backupKey(pathPrefix, name string, t time.Time, ext string) string {
	ts := t.UTC().Format("2006-01-02T15-04-05")
	base := fmt.Sprintf("%s/%s%s", name, ts, ext)
	if pathPrefix != "" {
		return strings.TrimSuffix(pathPrefix, "/") + "/" + base
	}
	return base
}

// backupListPrefix returns the S3 prefix used to list all objects for a given
// backup config, so retention can purge only the relevant objects.
func backupListPrefix(pathPrefix, name string) string {
	if pathPrefix != "" {
		return strings.TrimSuffix(pathPrefix, "/") + "/" + name + "/"
	}
	return name + "/"
}

// ─── DB status helpers ────────────────────────────────────────────────────────

func (s *BackupService) markStart(ctx context.Context, id uuid.UUID) {
	now := time.Now()
	s.db.WithContext(ctx).Model(&db.BackupConfig{}).Where("id = ?", id).Updates(map[string]any{
		"last_backup_status": db.BackupRunning,
		"last_backup_at":     now,
	})
}

func (s *BackupService) markEnd(ctx context.Context, id uuid.UUID, status db.BackupStatus) {
	s.db.WithContext(ctx).Model(&db.BackupConfig{}).Where("id = ?", id).
		Update("last_backup_status", status)
}

func (s *BackupService) markSysStart(ctx context.Context, id uuid.UUID) {
	now := time.Now()
	s.db.WithContext(ctx).Model(&db.SystemBackupConfig{}).Where("id = ?", id).Updates(map[string]any{
		"last_backup_status": db.BackupRunning,
		"last_backup_at":     now,
	})
}

func (s *BackupService) markSysEnd(ctx context.Context, id uuid.UUID, status db.BackupStatus) {
	s.db.WithContext(ctx).Model(&db.SystemBackupConfig{}).Where("id = ?", id).
		Update("last_backup_status", status)
}

// ─── Dump command builder ─────────────────────────────────────────────────────

// shellEsc single-quote-escapes a value for safe embedding in a sh -c string.
func shellEsc(s string) string {
	return strings.ReplaceAll(s, "'", `'"'"'`)
}

// dumpCmd returns the shell command to dump the database and write gzipped output
// to stdout. The command runs inside the existing DB pod (which has the dump tools).
// Returns an error for engines that are not yet supported.
func dumpCmd(engine db.DatabaseEngine, dbUser, dbName, dbPass string) ([]string, error) {
	u, n, p := shellEsc(dbUser), shellEsc(dbName), shellEsc(dbPass)
	switch engine {
	case db.DatabasePostgres:
		return []string{"sh", "-c", fmt.Sprintf(
			"PGPASSWORD='%s' pg_dump -U '%s' -d '%s' | gzip", p, u, n,
		)}, nil

	case db.DatabaseMySQL:
		return []string{"sh", "-c", fmt.Sprintf(
			"mysqldump -u '%s' -p'%s' '%s' | gzip", u, p, n,
		)}, nil

	case db.DatabaseMongoDB:
		// mongodump --archive writes a binary archive stream to stdout.
		return []string{"sh", "-c", fmt.Sprintf(
			"mongodump --authenticationDatabase admin -u '%s' -p '%s' -d '%s' --archive --gzip", u, p, n,
		)}, nil

	case db.DatabaseRedis, db.DatabaseDragonfly:
		if dbPass != "" {
			return []string{"sh", "-c", fmt.Sprintf(
				"redis-cli -a '%s' --rdb - 2>/dev/null | gzip", p,
			)}, nil
		}
		return []string{"sh", "-c", "redis-cli --rdb - 2>/dev/null | gzip"}, nil

	default:
		return nil, fmt.Errorf("unsupported engine for automated backup: %s", engine)
	}
}

// ─── Service DB backup ────────────────────────────────────────────────────────

// execBackup runs the full backup pipeline for a per-service BackupConfig.
// It is safe to call concurrently; the inFlight guard in backup.go prevents
// duplicate executions for the same config ID. The semaphore caps total
// concurrent backups across the entire service.
func (s *BackupService) execBackup(ctx context.Context, cfgID uuid.UUID) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()
	// 1. Load config with all needed associations.
	var cfg db.BackupConfig
	if err := s.db.WithContext(ctx).
		Preload("Service.Project").
		Preload("StorageIntegration").
		First(&cfg, "id = ?", cfgID).Error; err != nil {
		log.Printf("backup %s: load config: %v", cfgID, err)
		return
	}
	if !cfg.Enabled {
		return
	}

	// 2. Load DatabaseConfig (1:1 with Service, separate preload to avoid deep nesting).
	var dc db.DatabaseConfig
	if err := s.db.WithContext(ctx).Where("service_id = ?", cfg.ServiceID).First(&dc).Error; err != nil {
		log.Printf("backup %s: load database config: %v", cfgID, err)
		s.markEnd(ctx, cfgID, db.BackupFailed)
		return
	}

	s.markStart(ctx, cfgID)
	log.Printf("backup %s: starting (%s %s)", cfgID, dc.Engine, dc.DBName)

	// 3. Build dump command.
	cmd, err := dumpCmd(dc.Engine, dc.DBUser, dc.DBName, string(dc.DBPassword))
	if err != nil {
		log.Printf("backup %s: %v", cfgID, err)
		s.markEnd(ctx, cfgID, db.BackupFailed)
		return
	}

	// 4. K8s must be available to exec into the pod.
	if s.k8s == nil {
		log.Printf("backup %s: kubernetes not available", cfgID)
		s.markEnd(ctx, cfgID, db.BackupFailed)
		return
	}

	// 5. Find a running pod for this DB deployment.
	podName, err := appk8s.FindRunningPod(ctx, s.k8s, cfg.Service.Project.Slug, dc.Slug)
	if err != nil {
		log.Printf("backup %s: find pod: %v", cfgID, err)
		s.markEnd(ctx, cfgID, db.BackupFailed)
		return
	}

	// 6. Execute the dump command inside the pod; stream stdout back.
	// The container name equals the DB slug (set by ApplyDatabaseDeployment).
	reader, err := appk8s.ExecDumpCommand(ctx, s.k8s, s.restCfg,
		cfg.Service.Project.Slug, podName, dc.Slug, cmd)
	if err != nil {
		log.Printf("backup %s: exec dump: %v", cfgID, err)
		s.markEnd(ctx, cfgID, db.BackupFailed)
		return
	}
	defer reader.Close()

	// 7. Stream the dump directly into S3 (no temp file).
	s3c, err := newS3Client(cfg.StorageIntegration)
	if err != nil {
		log.Printf("backup %s: s3 client: %v", cfgID, err)
		s.markEnd(ctx, cfgID, db.BackupFailed)
		return
	}

	ext := ".sql.gz"
	if dc.Engine == db.DatabaseMongoDB {
		ext = ".dump.gz"
	}
	key := backupKey(cfg.PathPrefix, dc.Slug, time.Now(), ext)
	if err := uploadToS3(ctx, s3c, cfg.StorageIntegration.Bucket, key, reader); err != nil {
		log.Printf("backup %s: upload: %v", cfgID, err)
		s.markEnd(ctx, cfgID, db.BackupFailed)
		return
	}

	s.markEnd(ctx, cfgID, db.BackupSuccess)
	log.Printf("backup %s: success, key=%s", cfgID, key)

	// 8. Enforce retention: delete objects older than retention_days.
	purgeOldBackups(ctx, s3c, cfg.StorageIntegration.Bucket,
		backupListPrefix(cfg.PathPrefix, dc.Slug), cfg.RetentionDays)
}

// ─── System backup ────────────────────────────────────────────────────────────

// execSystemBackup dumps Meshploy's own PostgreSQL database using pg_dump from
// within the API process. Acquires the shared semaphore before running.
// within the API process. This requires postgresql-client in the runtime image
// (see apps/api/Dockerfile). The DATABASE_URL is used directly, so the dump
// connects through the same network path the API uses.
func (s *BackupService) execSystemBackup(ctx context.Context, cfgID uuid.UUID) {
	s.sem <- struct{}{}
	defer func() { <-s.sem }()

	var cfg db.SystemBackupConfig
	if err := s.db.WithContext(ctx).
		Preload("StorageIntegration").
		First(&cfg, "id = ?", cfgID).Error; err != nil {
		log.Printf("system-backup %s: load: %v", cfgID, err)
		return
	}
	if !cfg.Enabled {
		return
	}
	if s.cfg == nil || s.cfg.DatabaseURL == "" {
		log.Printf("system-backup %s: DATABASE_URL not set", cfgID)
		return
	}

	s.markSysStart(ctx, cfgID)
	log.Printf("system-backup %s: starting", cfgID)

	// Build the shell command: pg_dump <url> | gzip, streaming stdout to the pipe.
	safeURL := shellEsc(s.cfg.DatabaseURL)
	pr, pw := io.Pipe()

	shellCmd := exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("pg_dump '%s' | gzip", safeURL))
	shellCmd.Stdout = pw
	var stderrBuf bytes.Buffer
	shellCmd.Stderr = &stderrBuf

	go func() {
		if runErr := shellCmd.Run(); runErr != nil {
			pw.CloseWithError(fmt.Errorf("%w; stderr=%s", runErr, stderrBuf.String()))
		} else {
			pw.Close()
		}
	}()

	s3c, err := newS3Client(cfg.StorageIntegration)
	if err != nil {
		log.Printf("system-backup %s: s3 client: %v", cfgID, err)
		s.markSysEnd(ctx, cfgID, db.BackupFailed)
		pr.Close()
		return
	}

	key := backupKey(cfg.PathPrefix, "system", time.Now(), ".sql.gz")
	if err := uploadToS3(ctx, s3c, cfg.StorageIntegration.Bucket, key, pr); err != nil {
		log.Printf("system-backup %s: upload: %v", cfgID, err)
		s.markSysEnd(ctx, cfgID, db.BackupFailed)
		return
	}

	s.markSysEnd(ctx, cfgID, db.BackupSuccess)
	log.Printf("system-backup %s: success, key=%s", cfgID, key)

	purgeOldBackups(ctx, s3c, cfg.StorageIntegration.Bucket,
		backupListPrefix(cfg.PathPrefix, "system"), cfg.RetentionDays)
}
