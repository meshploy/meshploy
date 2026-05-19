package service

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/config"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	"github.com/meshploy/packages/db"
	cronparser "github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const maxConcurrentBackups = 3

type BackupService struct {
	db      *gorm.DB
	k8s     kubernetes.Interface
	restCfg *rest.Config
	cfg     *config.Config
	// inFlight prevents duplicate concurrent runs for the same backup config.
	// Key type: uuid.UUID for service backups, string "sys:<uuid>" for system backups.
	inFlight sync.Map
	// sem caps the number of simultaneously running backup jobs (across both
	// service and system backups). This bounds API memory and K8s pod CPU.
	sem chan struct{}
}

// BackupObject is a single restore point returned by ListObjects.
type BackupObject struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"last_modified"`
}

// ─── Per-service backup configs ───────────────────────────────────────────────

type CreateBackupInput struct {
	StorageIntegrationID uuid.UUID
	Schedule             string
	RetentionDays        int
	PathPrefix           string
}

type UpdateBackupInput struct {
	Schedule      *string
	RetentionDays *int
	PathPrefix    *string
	Enabled       *bool
}

func (s *BackupService) List(ctx context.Context, serviceID uuid.UUID) ([]db.BackupConfig, error) {
	items := make([]db.BackupConfig, 0)
	err := s.db.WithContext(ctx).Where("service_id = ?", serviceID).Find(&items).Error
	return items, err
}

func (s *BackupService) Create(ctx context.Context, orgID, serviceID uuid.UUID, in CreateBackupInput) (*db.BackupConfig, error) {
	var sto db.StorageIntegration
	if err := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", in.StorageIntegrationID, orgID).First(&sto).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("storage integration not found")
		}
		return nil, err
	}
	retentionDays := in.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}
	item := &db.BackupConfig{
		ServiceID:            serviceID,
		StorageIntegrationID: in.StorageIntegrationID,
		Schedule:             in.Schedule,
		RetentionDays:        retentionDays,
		PathPrefix:           in.PathPrefix,
		Enabled:              true,
	}
	return item, s.db.WithContext(ctx).Create(item).Error
}

func (s *BackupService) Update(ctx context.Context, id, serviceID uuid.UUID, in UpdateBackupInput) (*db.BackupConfig, error) {
	var item db.BackupConfig
	if err := s.db.WithContext(ctx).Where("id = ? AND service_id = ?", id, serviceID).First(&item).Error; err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if in.Schedule != nil {
		updates["schedule"] = *in.Schedule
	}
	if in.RetentionDays != nil {
		updates["retention_days"] = *in.RetentionDays
	}
	if in.PathPrefix != nil {
		updates["path_prefix"] = *in.PathPrefix
	}
	if in.Enabled != nil {
		updates["enabled"] = *in.Enabled
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&item).Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	return &item, nil
}

func (s *BackupService) Delete(ctx context.Context, id, serviceID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("id = ? AND service_id = ?", id, serviceID).
		Delete(&db.BackupConfig{}).Error
}

// Trigger marks the backup as pending and fires execBackup in a goroutine.
// The inFlight guard prevents a double-run if the scheduler is also active.
func (s *BackupService) Trigger(ctx context.Context, id, serviceID uuid.UUID) (*db.BackupConfig, error) {
	var item db.BackupConfig
	if err := s.db.WithContext(ctx).Where("id = ? AND service_id = ?", id, serviceID).First(&item).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&item).Updates(map[string]any{
		"last_backup_status": db.BackupPending,
		"last_backup_at":     now,
	}).Error; err != nil {
		return nil, err
	}

	if _, loaded := s.inFlight.LoadOrStore(item.ID, struct{}{}); !loaded {
		go func() {
			defer s.inFlight.Delete(item.ID)
			s.execBackup(context.Background(), item.ID)
		}()
	}
	return &item, nil
}

// ─── System backup config ─────────────────────────────────────────────────────

type UpsertSystemBackupInput struct {
	StorageIntegrationID uuid.UUID
	Schedule             string
	RetentionDays        int
	PathPrefix           string
	Enabled              bool
}

func (s *BackupService) GetSystem(ctx context.Context, orgID uuid.UUID) (*db.SystemBackupConfig, error) {
	var item db.SystemBackupConfig
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &item, err
}

func (s *BackupService) UpsertSystem(ctx context.Context, orgID uuid.UUID, in UpsertSystemBackupInput) (*db.SystemBackupConfig, error) {
	var sto db.StorageIntegration
	if err := s.db.WithContext(ctx).Where("id = ? AND organization_id = ?", in.StorageIntegrationID, orgID).First(&sto).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("storage integration not found")
		}
		return nil, err
	}
	retentionDays := in.RetentionDays
	if retentionDays <= 0 {
		retentionDays = 30
	}

	var item db.SystemBackupConfig
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		item = db.SystemBackupConfig{
			OrganizationID:       orgID,
			StorageIntegrationID: in.StorageIntegrationID,
			Schedule:             in.Schedule,
			RetentionDays:        retentionDays,
			PathPrefix:           in.PathPrefix,
			Enabled:              in.Enabled,
		}
		return &item, s.db.WithContext(ctx).Create(&item).Error
	}
	if err != nil {
		return nil, err
	}
	updates := map[string]any{
		"storage_integration_id": in.StorageIntegrationID,
		"schedule":               in.Schedule,
		"retention_days":         retentionDays,
		"path_prefix":            in.PathPrefix,
		"enabled":                in.Enabled,
	}
	return &item, s.db.WithContext(ctx).Model(&item).Updates(updates).Error
}

func (s *BackupService) DeleteSystem(ctx context.Context, orgID uuid.UUID) error {
	return s.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		Delete(&db.SystemBackupConfig{}).Error
}

// TriggerSystem marks the system backup pending and fires execSystemBackup.
func (s *BackupService) TriggerSystem(ctx context.Context, orgID uuid.UUID) (*db.SystemBackupConfig, error) {
	var item db.SystemBackupConfig
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&item).Error; err != nil {
		return nil, err
	}
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&item).Updates(map[string]any{
		"last_backup_status": db.BackupPending,
		"last_backup_at":     now,
	}).Error; err != nil {
		return nil, err
	}

	sysKey := "sys:" + item.ID.String()
	if _, loaded := s.inFlight.LoadOrStore(sysKey, struct{}{}); !loaded {
		go func() {
			defer s.inFlight.Delete(sysKey)
			s.execSystemBackup(context.Background(), item.ID)
		}()
	}
	return &item, nil
}

// ─── Scheduler ────────────────────────────────────────────────────────────────

// StartScheduler ticks every minute and runs any backup configs whose cron
// schedule is overdue. Safe to call once at startup in a goroutine.
func (s *BackupService) StartScheduler(ctx context.Context) {
	// Ensure K8s shell pods from crashed sessions are cleaned before first tick.
	if s.k8s != nil {
		go appk8s.CleanupOrphanedShellPods(ctx, s.k8s)
	}

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.schedulerTick(ctx)
		}
	}
}

func (s *BackupService) schedulerTick(ctx context.Context) {
	now := time.Now()

	// ── Per-service backups ──────────────────────────────────────────────────
	var cfgs []db.BackupConfig
	if err := s.db.WithContext(ctx).Where("enabled = true").Find(&cfgs).Error; err != nil {
		log.Printf("backup scheduler: query: %v", err)
		return
	}
	for _, cfg := range cfgs {
		if !isDue(cfg.Schedule, cfg.LastBackupAt, cfg.CreatedAt, now) {
			continue
		}
		if _, loaded := s.inFlight.LoadOrStore(cfg.ID, struct{}{}); loaded {
			continue
		}
		id := cfg.ID
		go func() {
			defer s.inFlight.Delete(id)
			s.execBackup(context.Background(), id)
		}()
	}

	// ── System backups ───────────────────────────────────────────────────────
	var sysCfgs []db.SystemBackupConfig
	if err := s.db.WithContext(ctx).Where("enabled = true").Find(&sysCfgs).Error; err != nil {
		return
	}
	for _, cfg := range sysCfgs {
		if !isDue(cfg.Schedule, cfg.LastBackupAt, cfg.CreatedAt, now) {
			continue
		}
		sysKey := "sys:" + cfg.ID.String()
		if _, loaded := s.inFlight.LoadOrStore(sysKey, struct{}{}); loaded {
			continue
		}
		cfgID := cfg.ID
		go func() {
			defer s.inFlight.Delete(sysKey)
			s.execSystemBackup(context.Background(), cfgID)
		}()
	}
}

// StartRetentionReaper runs once at startup then every 24 hours. It enforces
// retention_days for every backup config regardless of whether a backup ran
// recently — handles changed policies, long-failing configs, and disabled
// configs that still have old objects in S3.
func (s *BackupService) StartRetentionReaper(ctx context.Context) {
	// Short initial delay to let the API finish starting up before hitting S3.
	select {
	case <-ctx.Done():
		return
	case <-time.After(2 * time.Minute):
	}

	s.reapAll(ctx)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.reapAll(ctx)
		}
	}
}

func (s *BackupService) reapAll(ctx context.Context) {
	// ── Per-service backup configs ───────────────────────────────────────────
	var cfgs []db.BackupConfig
	if err := s.db.WithContext(ctx).
		Preload("StorageIntegration").
		Preload("Service").
		Where("retention_days > 0").
		Find(&cfgs).Error; err != nil {
		log.Printf("retention reaper: query backup_configs: %v", err)
		return
	}
	for _, cfg := range cfgs {
		s3c, err := newS3Client(cfg.StorageIntegration)
		if err != nil {
			log.Printf("retention reaper: s3 client for backup %s: %v", cfg.ID, err)
		} else {
			var dc db.DatabaseConfig
			if err := s.db.WithContext(ctx).Where("service_id = ?", cfg.ServiceID).First(&dc).Error; err != nil {
				log.Printf("retention reaper: db config for backup %s: %v", cfg.ID, err)
			} else {
				purgeOldBackups(ctx, s3c, cfg.StorageIntegration.Bucket,
					backupListPrefix(cfg.PathPrefix, dc.Slug), cfg.RetentionDays)
			}
		}
		// Brief pause between configs to avoid bursting S3 API rate limits.
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}

	// ── System backup configs ────────────────────────────────────────────────
	var sysCfgs []db.SystemBackupConfig
	if err := s.db.WithContext(ctx).
		Preload("StorageIntegration").
		Where("retention_days > 0").
		Find(&sysCfgs).Error; err != nil {
		log.Printf("retention reaper: query system_backup_configs: %v", err)
		return
	}
	for _, cfg := range sysCfgs {
		s3c, err := newS3Client(cfg.StorageIntegration)
		if err != nil {
			log.Printf("retention reaper: s3 client for system backup %s: %v", cfg.ID, err)
		} else {
			purgeOldBackups(ctx, s3c, cfg.StorageIntegration.Bucket,
				backupListPrefix(cfg.PathPrefix, "system"), cfg.RetentionDays)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// ─── Restore ─────────────────────────────────────────────────────────────────

// ListObjects returns all available restore points for a backup config, newest first.
func (s *BackupService) ListObjects(ctx context.Context, id, serviceID uuid.UUID) ([]BackupObject, error) {
	var cfg db.BackupConfig
	if err := s.db.WithContext(ctx).
		Preload("StorageIntegration").
		Where("id = ? AND service_id = ?", id, serviceID).First(&cfg).Error; err != nil {
		return nil, err
	}
	var dc db.DatabaseConfig
	if err := s.db.WithContext(ctx).Where("service_id = ?", cfg.ServiceID).First(&dc).Error; err != nil {
		return nil, err
	}
	s3c, err := newS3Client(cfg.StorageIntegration)
	if err != nil {
		return nil, err
	}
	return listBackupObjects(ctx, s3c, cfg.StorageIntegration.Bucket,
		backupListPrefix(cfg.PathPrefix, dc.Slug))
}

// Restore asynchronously restores the database from the given S3 key.
// Returns immediately with 202; the restore runs in the background.
func (s *BackupService) Restore(ctx context.Context, id, serviceID uuid.UUID, key string) error {
	// Validate the config exists before accepting.
	var cfg db.BackupConfig
	if err := s.db.WithContext(ctx).Where("id = ? AND service_id = ?", id, serviceID).First(&cfg).Error; err != nil {
		return err
	}
	go func() {
		if err := s.execRestore(context.Background(), id, key); err != nil {
			log.Printf("restore %s key=%s: %v", id, key, err)
		} else {
			log.Printf("restore %s key=%s: success", id, key)
		}
	}()
	return nil
}

// ListSystemObjects returns all available restore points for the system backup, newest first.
func (s *BackupService) ListSystemObjects(ctx context.Context, orgID uuid.UUID) ([]BackupObject, error) {
	var cfg db.SystemBackupConfig
	if err := s.db.WithContext(ctx).
		Preload("StorageIntegration").
		Where("organization_id = ?", orgID).First(&cfg).Error; err != nil {
		return nil, err
	}
	s3c, err := newS3Client(cfg.StorageIntegration)
	if err != nil {
		return nil, err
	}
	return listBackupObjects(ctx, s3c, cfg.StorageIntegration.Bucket,
		backupListPrefix(cfg.PathPrefix, "system"))
}

// RestoreSystem asynchronously restores the system database from the given S3 key.
func (s *BackupService) RestoreSystem(ctx context.Context, orgID uuid.UUID, key string) error {
	var cfg db.SystemBackupConfig
	if err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).First(&cfg).Error; err != nil {
		return err
	}
	go func() {
		if err := s.execSystemRestore(context.Background(), cfg.ID, key); err != nil {
			log.Printf("system-restore org=%s key=%s: %v", orgID, key, err)
		} else {
			log.Printf("system-restore org=%s key=%s: success", orgID, key)
		}
	}()
	return nil
}

// isDue returns true if the cron schedule is overdue relative to the last run
// (or the config's creation time if it has never run).
func isDue(schedule string, lastAt *time.Time, createdAt time.Time, now time.Time) bool {
	sched, err := cronparser.ParseStandard(schedule)
	if err != nil {
		log.Printf("backup scheduler: invalid schedule %q: %v", schedule, err)
		return false
	}
	ref := createdAt
	if lastAt != nil {
		ref = *lastAt
	}
	return now.After(sched.Next(ref))
}
