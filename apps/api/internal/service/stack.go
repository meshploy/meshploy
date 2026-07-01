package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"gorm.io/gorm"
)

type StackService struct {
	db       *gorm.DB
	workload *WorkloadService
	volumes  *VolumeService
}

// ---------------------------------------------------------------------------
// Input types
// ---------------------------------------------------------------------------

type CreateStackInput struct {
	Name      string
	Spec      string
	Variables map[string]string

	// Git source — set when git_mode != ""
	GitMode          meshdb.StackGitMode
	GitRepo          string
	GitBranch        string
	GitPath          string
	GitIntegrationID *uuid.UUID

	// Template provenance — set when the stack is created from a template.
	TemplateID      string
	TemplateVersion string
}

type UpdateStackInput struct {
	Name      string
	Spec      string
	Variables map[string]string

	// Git source
	GitMode          meshdb.StackGitMode
	GitRepo          string
	GitBranch        string
	GitPath          string
	UpdateGitIntegration  bool       // true = apply GitIntegrationID (even if nil, to clear it)
	GitIntegrationID *uuid.UUID
}

// SyncResult is returned by Sync after fetching and applying the spec from git.
type SyncResult struct {
	*ApplyResult
	SuggestedMode meshdb.StackGitMode // non-empty when current mode seems wrong
	Warning       string              // human-readable explanation for SuggestedMode
}

// ---------------------------------------------------------------------------
// CRUD
// ---------------------------------------------------------------------------

func (s *StackService) List(ctx context.Context, projectID uuid.UUID) ([]meshdb.Stack, error) {
	var stacks []meshdb.Stack
	err := s.db.WithContext(ctx).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Find(&stacks).Error
	return stacks, err
}

func (s *StackService) Get(ctx context.Context, stackID, projectID uuid.UUID) (*meshdb.Stack, error) {
	var stack meshdb.Stack
	err := s.db.WithContext(ctx).First(&stack, "id = ? AND project_id = ?", stackID, projectID).Error
	return &stack, err
}

func (s *StackService) getByID(ctx context.Context, stackID uuid.UUID) (*meshdb.Stack, error) {
	var stack meshdb.Stack
	err := s.db.WithContext(ctx).First(&stack, "id = ?", stackID).Error
	return &stack, err
}

func (s *StackService) Create(ctx context.Context, projectID uuid.UUID, in CreateStackInput) (*meshdb.Stack, error) {
	if in.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	gitPath := in.GitPath
	if gitPath == "" {
		gitPath = "docker-compose.yml"
	}
	gitBranch := in.GitBranch
	if gitBranch == "" {
		gitBranch = "main"
	}
	vars := strMapToJSONObj(in.Variables)
	stack := &meshdb.Stack{
		ProjectID:        projectID,
		Name:             in.Name,
		Spec:             in.Spec,
		Variables:        vars,
		Status:           meshdb.StackIdle,
		GitMode:          in.GitMode,
		GitRepo:          in.GitRepo,
		GitBranch:        gitBranch,
		GitPath:          gitPath,
		GitIntegrationID: in.GitIntegrationID,
		TemplateID:       in.TemplateID,
		TemplateVersion:  in.TemplateVersion,
	}
	err := s.db.WithContext(ctx).Create(stack).Error
	return stack, err
}

func (s *StackService) Update(ctx context.Context, stackID uuid.UUID, in UpdateStackInput) (*meshdb.Stack, error) {
	var stack meshdb.Stack
	if err := s.db.WithContext(ctx).First(&stack, "id = ?", stackID).Error; err != nil {
		return nil, err
	}
	updates := map[string]any{
		"spec":       in.Spec,
		"git_mode":   in.GitMode,
		"git_repo":   in.GitRepo,
		"git_branch": in.GitBranch,
		"git_path":   in.GitPath,
	}
	if in.Name != "" {
		updates["name"] = in.Name
	}
	if in.Variables != nil {
		updates["variables"] = strMapToJSONObj(in.Variables)
	}
	if in.UpdateGitIntegration {
		updates["git_integration_id"] = in.GitIntegrationID
	}
	if err := s.db.WithContext(ctx).Model(&stack).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.getByID(ctx, stackID)
}

func (s *StackService) Delete(ctx context.Context, stackID uuid.UUID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		tx.Where("resource_type = ? AND resource_id = ?", meshdb.ResourceStack, stackID).
			Delete(&meshdb.ResourcePermission{})
		return tx.Delete(&meshdb.Stack{}, "id = ?", stackID).Error
	})
}

// Sync fetches the compose spec from the stack's git source, updates Spec,
// detects mode mismatches, then calls Apply.
func (s *StackService) Sync(ctx context.Context, stackID uuid.UUID, triggeredBy uuid.UUID) (*SyncResult, error) {
	var stack meshdb.Stack
	if err := s.db.WithContext(ctx).Preload("GitIntegration").First(&stack, "id = ?", stackID).Error; err != nil {
		return nil, err
	}
	if stack.GitMode == meshdb.StackGitModeRaw || stack.GitRepo == "" {
		return nil, fmt.Errorf("stack has no git source configured")
	}

	spec, sha, err := s.fetchSpec(ctx, &stack)
	if err != nil {
		return nil, fmt.Errorf("fetch spec: %w", err)
	}

	suggestedMode, warning := detectModeMismatch(stack.GitMode, spec)

	if err := s.db.WithContext(ctx).Model(&stack).Updates(map[string]any{
		"spec":               spec,
		"git_last_synced_at": time.Now(),
		"git_last_sync_sha":  sha,
	}).Error; err != nil {
		return nil, err
	}
	stack.Spec = spec

	applyResult, err := s.Apply(ctx, stackID, triggeredBy, nil)
	if err != nil {
		return nil, err
	}
	return &SyncResult{
		ApplyResult:   applyResult,
		SuggestedMode: suggestedMode,
		Warning:       warning,
	}, nil
}

// fetchSpec retrieves the compose file content from the stack's git source.
// Returns the spec content and the commit SHA (best-effort; empty string if unavailable).
func (s *StackService) fetchSpec(ctx context.Context, stack *meshdb.Stack) (spec, sha string, err error) {
	token := ""
	if stack.GitIntegration != nil {
		gi := stack.GitIntegration
		if gi.GHAppID != "" && string(gi.InstallationID) != "" {
			token, err = getInstallationToken(gi.GHAppID, string(gi.GHPrivateKey), string(gi.InstallationID))
			if err != nil {
				return "", "", fmt.Errorf("get github token: %w", err)
			}
		} else {
			// PAT or OAuth access token stored in InstallationID field
			token = string(gi.InstallationID)
		}
	}

	switch stack.GitMode {
	case meshdb.StackGitModeFile:
		spec, sha, err = fetchRawFile(ctx, stack.GitRepo, stack.GitBranch, stack.GitPath, token)
	case meshdb.StackGitModeRepo:
		spec, sha, err = cloneAndReadFile(ctx, stack.GitRepo, stack.GitBranch, stack.GitPath, token)
	default:
		err = fmt.Errorf("unsupported git mode: %q", stack.GitMode)
	}
	return
}

// fetchRawFile fetches a single file from a git repo via the GitHub/GitLab raw-content API.
func fetchRawFile(ctx context.Context, repoURL, branch, filePath, token string) (content, sha string, err error) {
	rawURL, err := toRawURL(repoURL, branch, filePath)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", "", err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("fetch %s: HTTP %d", rawURL, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	// SHA not available from raw file fetch; caller accepts empty string.
	return string(body), "", nil
}

// cloneAndReadFile does a shallow clone and reads the target file from the working tree.
func cloneAndReadFile(ctx context.Context, repoURL, branch, filePath, token string) (content, sha string, err error) {
	dir, err := cloneRepo(ctx, repoURL, branch, token)
	if err != nil {
		return "", "", err
	}
	data, err := readFileFromClone(dir, filePath)
	if err != nil {
		return "", "", fmt.Errorf("read %s from clone: %w", filePath, err)
	}
	sha, _ = headSHA(dir)
	return string(data), sha, nil
}

// detectModeMismatch parses the spec and advises whether the current git mode is appropriate.
func detectModeMismatch(mode meshdb.StackGitMode, spec string) (suggestedMode meshdb.StackGitMode, warning string) {
	hasLocalContext := specHasLocalBuildContext(spec)
	switch {
	case mode == meshdb.StackGitModeFile && hasLocalContext:
		return meshdb.StackGitModeRepo,
			"This compose file uses local build contexts (build.context with relative paths). Switch to 'Whole repo' mode so build jobs can access the source directories."
	case mode == meshdb.StackGitModeRepo && !hasLocalContext:
		return meshdb.StackGitModeFile,
			"This compose file has no local build contexts. 'Compose file only' mode is sufficient and avoids cloning the full repository."
	}
	return "", ""
}

// specHasLocalBuildContext returns true when any service in the spec
// uses a relative build.context path (indicating the full repo is needed).
func specHasLocalBuildContext(spec string) bool {
	project, err := loader.LoadWithContext(context.Background(), composetypes.ConfigDetails{
		WorkingDir:  "/",
		ConfigFiles: []composetypes.ConfigFile{{Filename: "docker-compose.yml", Content: []byte(spec)}},
	}, loader.WithSkipValidation)
	if err != nil {
		return false
	}
	for _, svc := range project.Services {
		if svc.Build != nil {
			ctx := svc.Build.Context
			if ctx != "" && !strings.HasPrefix(ctx, "http://") && !strings.HasPrefix(ctx, "https://") {
				return true
			}
		}
	}
	return false
}

func (s *StackService) ListServices(ctx context.Context, stackID uuid.UUID) ([]meshdb.Service, error) {
	var services []meshdb.Service
	err := s.db.WithContext(ctx).
		Where("stack_id = ?", stackID).
		Order("name ASC").
		Find(&services).Error
	return services, err
}

// ---------------------------------------------------------------------------
// x-meshploy extension types (decoded from compose Extensions map)
// ---------------------------------------------------------------------------

type meshployExt struct {
	Type     string            `json:"type"`
	Source   *meshploySource   `json:"source"`
	Build    *meshployBuild    `json:"build"`
	Deploy   *meshployDeploy   `json:"deploy"`
	Rollback *meshployRollback `json:"rollback"`
	Database *meshployDatabase `json:"database"`
}

type meshployDatabase struct {
	Engine     string `json:"engine"`
	Version    string `json:"version"`
	StorageGB  int    `json:"storage_gb"`
	DBName     string `json:"db_name"`
	DBUser     string `json:"db_user"`
	DBPassword string `json:"db_password"`
}

type meshploySource struct {
	Git           string `json:"git"`
	Branch        string `json:"branch"`
	RootDir       string `json:"root_dir"`
	IntegrationID string `json:"integration_id"`
}

type meshployBuild struct {
	Builder              string `json:"builder"`
	DockerfilePath       string `json:"dockerfile_path"`
	BuilderNode          string `json:"builder_node"`
	BuilderCPURequest    string `json:"builder_cpu_request"`
	BuilderMemoryRequest string `json:"builder_memory_request"`
}

type meshployDeploy struct {
	Replicas      int    `json:"replicas"`
	Port          int    `json:"port"`
	Node          string `json:"node"`
	CPURequest    string `json:"cpu_request"`
	CPULimit      string `json:"cpu_limit"`
	MemoryRequest string `json:"memory_request"`
	MemoryLimit   string `json:"memory_limit"`
}

type meshployRollback struct {
	Enabled   bool `json:"enabled"`
	Retention int  `json:"retention"`
}

// ---------------------------------------------------------------------------
// Apply result
// ---------------------------------------------------------------------------

type ApplyResult struct {
	Stack   *meshdb.Stack
	Created []string
	Updated []string
	Deleted []string
	Errors  []string
}

// ---------------------------------------------------------------------------
// Apply — reconcile DB records from compose-go parsed spec
// ---------------------------------------------------------------------------

func (s *StackService) Apply(ctx context.Context, stackID uuid.UUID, triggerBy uuid.UUID, envOverrides map[string]string) (*ApplyResult, error) {
	var stack meshdb.Stack
	if err := s.db.WithContext(ctx).First(&stack, "id = ?", stackID).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&stack).Updates(map[string]any{"status": meshdb.StackApplying}).Error; err != nil {
		return nil, err
	}

	result := &ApplyResult{Stack: &stack}

	// Build environment map: stored variables + one-shot overrides.
	envMap := jsonObjToStrMap(stack.Variables)
	for k, v := range envOverrides {
		envMap[k] = v
	}

	project, err := loader.LoadWithContext(ctx, composetypes.ConfigDetails{
		WorkingDir:  "/",
		ConfigFiles: []composetypes.ConfigFile{{Filename: "docker-compose.yml", Content: []byte(stack.Spec)}},
		Environment: envMap,
	}, loader.WithSkipValidation)
	if err != nil {
		_ = s.db.WithContext(ctx).Model(&stack).Updates(map[string]any{"status": meshdb.StackFailed}).Error
		return nil, fmt.Errorf("invalid spec: %w", err)
	}

	// Resolve top-level named volumes → PVC records.
	volumesByName := s.resolveNamedVolumes(ctx, stack.ProjectID, stack.Name, project.Volumes, result)

	// Current services in this stack.
	var existing []meshdb.Service
	s.db.WithContext(ctx).Where("stack_id = ?", stackID).Find(&existing)
	existingByName := make(map[string]meshdb.Service, len(existing))
	for _, svc := range existing {
		existingByName[svc.Name] = svc
	}

	specNames := make(map[string]struct{}, len(project.Services))

	for _, svcName := range topoSortServices(project.Services) {
		svcDef := project.Services[svcName]
		specNames[svcName] = struct{}{}

		ext := decodeExt(svcDef.Extensions)
		isDatabase := ext != nil && ext.Type == "database"
		envVarsStr := envFromMapping(svcDef.Environment)
		hcCmd, hcInterval, hcTimeout, hcRetries, hcStartPeriod := healthcheckFromCompose(svcDef.HealthCheck)

		port := 0
		if ext != nil && ext.Deploy != nil && ext.Deploy.Port > 0 {
			port = ext.Deploy.Port
		}
		if port == 0 && len(svcDef.Ports) > 0 {
			port = int(svcDef.Ports[0].Target)
		}
		if port == 0 {
			port = 3000
		}

		replicas := 1
		cpuRequest, cpuLimit := "100m", "500m"
		memRequest, memLimit := "128Mi", "512Mi"
		var nodeID *uuid.UUID
		if ext != nil && ext.Deploy != nil {
			if ext.Deploy.Replicas > 0 {
				replicas = ext.Deploy.Replicas
			}
			if ext.Deploy.CPURequest != "" {
				cpuRequest = ext.Deploy.CPURequest
			}
			if ext.Deploy.CPULimit != "" {
				cpuLimit = ext.Deploy.CPULimit
			}
			if ext.Deploy.MemoryRequest != "" {
				memRequest = ext.Deploy.MemoryRequest
			}
			if ext.Deploy.MemoryLimit != "" {
				memLimit = ext.Deploy.MemoryLimit
			}
			if ext.Deploy.Node != "" {
				if id, err := uuid.Parse(ext.Deploy.Node); err == nil {
					nodeID = &id
				}
			}
		}

		existingSvc, exists := existingByName[svcName]
		if !exists {
			var svc *meshdb.Service
			var createErr error

			if isDatabase && ext.Database != nil {
				dbInput := CreateWorkloadInput{
					StackID:                    &stackID,
					Name:                       svcName,
					Type:                       meshdb.ServiceTypeDatabase,
					Replicas:                   replicas,
					Engine:                     meshdb.DatabaseEngine(ext.Database.Engine),
					Version:                    ext.Database.Version,
					StorageGB:                  ext.Database.StorageGB,
					DBName:                     ext.Database.DBName,
					DBUser:                     ext.Database.DBUser,
					DBPassword:                 ext.Database.DBPassword,
					HealthcheckCmd:             hcCmd,
					HealthcheckIntervalSecs:    hcInterval,
					HealthcheckTimeoutSecs:     hcTimeout,
					HealthcheckRetries:         hcRetries,
					HealthcheckStartPeriodSecs: hcStartPeriod,
				}
				if dbInput.StorageGB == 0 {
					dbInput.StorageGB = 10
				}
				svc, createErr = s.workload.Create(ctx, stack.ProjectID, dbInput)
			} else {
				input := CreateWorkloadInput{
					StackID:   &stackID,
					Name:      svcName,
					Type:      meshdb.ServiceTypeApplication,
					Image:     svcDef.Image,
					Ports: []PortInput{{
						Name:      "http",
						Port:      port,
						IsHTTP:    true,
						IsPrimary: true,
						IsPublic:  true,
					}},
					Replicas:                   replicas,
					CPURequest:                 cpuRequest,
					CPULimit:                   cpuLimit,
					MemoryRequest:              memRequest,
					MemoryLimit:                memLimit,
					EnvVars:                    envVarsStr,
					NodeID:                     nodeID,
					HealthcheckCmd:             hcCmd,
					HealthcheckIntervalSecs:    hcInterval,
					HealthcheckTimeoutSecs:     hcTimeout,
					HealthcheckRetries:         hcRetries,
					HealthcheckStartPeriodSecs: hcStartPeriod,
				}
				svc, createErr = s.workload.Create(ctx, stack.ProjectID, input)
			}
			if createErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: create failed: %v", svcName, createErr))
				continue
			}

			hasBuildContext := svcDef.Build != nil && svcDef.Build.Context != ""
			if !isDatabase && (ext != nil && (ext.Source != nil || ext.Build != nil) || hasBuildContext) {
				if err := s.applyBuildConfig(ctx, svc.ID, svcDef, ext, &stack); err != nil {
					result.Errors = append(result.Errors, fmt.Sprintf("%s: build config failed: %v", svcName, err))
				}
			}

			s.attachVolumeMounts(ctx, svc.ID, svcDef.Volumes, volumesByName)
			result.Created = append(result.Created, svcName)
		} else {
			updates := map[string]any{
				"image":                         svcDef.Image,
				"replicas":                      replicas,
				"cpu_request":                   cpuRequest,
				"cpu_limit":                     cpuLimit,
				"memory_request":                memRequest,
				"memory_limit":                  memLimit,
				"env_vars":                      envVarsStr,
				"healthcheck_cmd":               hcCmd,
				"healthcheck_interval_secs":     hcInterval,
				"healthcheck_timeout_secs":      hcTimeout,
				"healthcheck_retries":           hcRetries,
				"healthcheck_start_period_secs": hcStartPeriod,
			}
			if err := s.db.WithContext(ctx).Model(&existingSvc).Updates(updates).Error; err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: update failed: %v", svcName, err))
				continue
			}
			s.attachVolumeMounts(ctx, existingSvc.ID, svcDef.Volumes, volumesByName)
			result.Updated = append(result.Updated, svcName)
		}
	}

	// Unlink services no longer in spec.
	for name, svc := range existingByName {
		if _, ok := specNames[name]; !ok {
			s.db.WithContext(ctx).Model(&svc).Update("stack_id", nil)
			result.Deleted = append(result.Deleted, name)
		}
	}

	now := time.Now()
	finalStatus := meshdb.StackIdle
	if len(result.Errors) > 0 {
		finalStatus = meshdb.StackFailed
	}
	s.db.WithContext(ctx).Model(&stack).Updates(map[string]any{
		"status":          finalStatus,
		"last_applied_at": now,
	})
	stack.Status = finalStatus
	stack.LastAppliedAt = &now
	result.Stack = &stack

	return result, nil
}

// ---------------------------------------------------------------------------
// Volume helpers
// ---------------------------------------------------------------------------

func (s *StackService) resolveNamedVolumes(
	ctx context.Context,
	projectID uuid.UUID,
	stackName string,
	composeVolumes composetypes.Volumes,
	result *ApplyResult,
) map[string]*meshdb.Volume {
	out := make(map[string]*meshdb.Volume, len(composeVolumes))
	if s.volumes == nil {
		return out
	}
	for volName := range composeVolumes {
		storedName := stackName + "-" + volName
		var existing meshdb.Volume
		if err := s.db.WithContext(ctx).
			Where("project_id = ? AND name = ?", projectID, storedName).
			First(&existing).Error; err == nil {
			out[volName] = &existing
			continue
		}
		created, err := s.volumes.Create(ctx, projectID, storedName, 5)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("volume %s: %v", volName, err))
			continue
		}
		out[volName] = created
	}
	return out
}

func (s *StackService) attachVolumeMounts(
	ctx context.Context,
	serviceID uuid.UUID,
	mounts []composetypes.ServiceVolumeConfig,
	volumesByName map[string]*meshdb.Volume,
) {
	if s.volumes == nil {
		return
	}
	for _, m := range mounts {
		if m.Type != "volume" || m.Source == "" {
			continue
		}
		vol, ok := volumesByName[m.Source]
		if !ok {
			continue
		}
		var existing meshdb.VolumeMount
		if s.db.WithContext(ctx).
			Where("volume_id = ? AND service_id = ?", vol.ID, serviceID).
			First(&existing).Error == nil {
			continue // already attached
		}
		s.volumes.Attach(ctx, vol.ID, serviceID, m.Target) //nolint:errcheck
	}
}

// ---------------------------------------------------------------------------
// Build config
// ---------------------------------------------------------------------------

func (s *StackService) applyBuildConfig(ctx context.Context, serviceID uuid.UUID, svcDef composetypes.ServiceConfig, ext *meshployExt, stack *meshdb.Stack) error {
	builder := meshdb.BuilderNixpacks
	if ext != nil && ext.Build != nil && ext.Build.Builder != "" {
		switch ext.Build.Builder {
		case "railpack":
			builder = meshdb.BuilderRailpack
		case "dockerfile":
			builder = meshdb.BuilderDockerfile
		case "image":
			builder = meshdb.BuilderImage
		}
	}
	if svcDef.Image != "" && (ext == nil || ext.Source == nil || ext.Source.Git == "") {
		builder = meshdb.BuilderImage
	}

	input := UpdateBuildConfigInput{Builder: &builder}

	if ext != nil && ext.Source != nil && ext.Source.Git != "" {
		// Service has its own explicit git source via x-meshploy.
		gitRepo := ext.Source.Git
		input.GitRepo = &gitRepo
		if ext.Source.Branch != "" {
			branch := ext.Source.Branch
			input.Branch = &branch
		}
		if ext.Source.IntegrationID != "" {
			if id, err := uuid.Parse(ext.Source.IntegrationID); err == nil {
				input.GitIntegrationID = &id
			}
		}
	} else if stack != nil && stack.GitRepo != "" && svcDef.Build != nil {
		// Service has a native build.context — inherit the stack's git source.
		gitRepo := stack.GitRepo
		branch := stack.GitBranch
		input.GitRepo = &gitRepo
		input.Branch = &branch
		if stack.GitIntegrationID != nil {
			input.GitIntegrationID = stack.GitIntegrationID
		}
	}

	// Set RootDir from the native build.context (strip leading ./ or /).
	if svcDef.Build != nil && svcDef.Build.Context != "" {
		rootDir := strings.TrimPrefix(svcDef.Build.Context, "./")
		rootDir = strings.TrimPrefix(rootDir, "/")
		if rootDir == "." {
			rootDir = ""
		}
		input.RootDir = &rootDir
	}

	if ext != nil && ext.Build != nil {
		if ext.Build.DockerfilePath != "" {
			input.DockerfilePath = &ext.Build.DockerfilePath
		}
		if ext.Build.BuilderNode != "" {
			input.BuilderNode = &ext.Build.BuilderNode
		}
		if ext.Build.BuilderCPURequest != "" {
			input.BuilderCPURequest = &ext.Build.BuilderCPURequest
		}
		if ext.Build.BuilderMemoryRequest != "" {
			input.BuilderMemoryRequest = &ext.Build.BuilderMemoryRequest
		}
	}
	if ext != nil && ext.Rollback != nil {
		input.RollbackEnabled = &ext.Rollback.Enabled
		if ext.Rollback.Retention > 0 {
			input.ImageRetention = &ext.Rollback.Retention
		}
	}

	_, err := s.workload.UpsertBuildConfig(ctx, serviceID, input)
	return err
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// topoSortServices returns service names in dependency order (dependencies first).
func topoSortServices(services composetypes.Services) []string {
	inDegree := make(map[string]int, len(services))
	successors := make(map[string][]string, len(services))

	for name := range services {
		if _, ok := inDegree[name]; !ok {
			inDegree[name] = 0
		}
	}
	for name, svc := range services {
		for dep := range svc.DependsOn {
			successors[dep] = append(successors[dep], name)
			inDegree[name]++
		}
	}

	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}
	sort.Strings(queue)

	var result []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)
		next := successors[node]
		sort.Strings(next)
		for _, dep := range next {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
				sort.Strings(queue)
			}
		}
	}

	// Cycle fallback: append any unvisited nodes.
	visited := make(map[string]struct{}, len(result))
	for _, n := range result {
		visited[n] = struct{}{}
	}
	var remaining []string
	for name := range services {
		if _, ok := visited[name]; !ok {
			remaining = append(remaining, name)
		}
	}
	sort.Strings(remaining)
	return append(result, remaining...)
}

func healthcheckFromCompose(hc *composetypes.HealthCheckConfig) (cmd string, interval, timeout, retries, startPeriod int32) {
	if hc == nil || hc.Disable || len(hc.Test) == 0 {
		return
	}
	var execCmd []string
	switch hc.Test[0] {
	case "CMD":
		execCmd = hc.Test[1:]
	case "CMD-SHELL":
		if len(hc.Test) >= 2 {
			execCmd = []string{"/bin/sh", "-c", strings.Join(hc.Test[1:], " ")}
		}
	default:
		execCmd = hc.Test
	}
	if len(execCmd) > 0 {
		if b, err := json.Marshal(execCmd); err == nil {
			cmd = string(b)
		}
	}
	if hc.Interval != nil {
		interval = int32(time.Duration(*hc.Interval).Seconds())
	}
	if hc.Timeout != nil {
		timeout = int32(time.Duration(*hc.Timeout).Seconds())
	}
	if hc.Retries != nil {
		retries = int32(*hc.Retries)
	}
	if hc.StartPeriod != nil {
		startPeriod = int32(time.Duration(*hc.StartPeriod).Seconds())
	}
	return
}

func envFromMapping(m composetypes.MappingWithEquals) string {
	parts := make([]string, 0, len(m))
	for k, v := range m {
		if v != nil {
			parts = append(parts, k+"="+*v)
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "\n")
}

func decodeExt(exts composetypes.Extensions) *meshployExt {
	raw, ok := exts["x-meshploy"]
	if !ok {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var ext meshployExt
	if err := json.Unmarshal(b, &ext); err != nil {
		return nil
	}
	return &ext
}

// ---------------------------------------------------------------------------
// Git helpers
// ---------------------------------------------------------------------------

// toRawURL converts a repo URL + branch + file path to a raw-content URL
// for GitHub (github.com → raw.githubusercontent.com) and GitLab (raw API).
func toRawURL(repoURL, branch, filePath string) (string, error) {
	filePath = strings.TrimPrefix(filePath, "/")
	switch {
	case strings.Contains(repoURL, "github.com"):
		// https://github.com/owner/repo  →  https://raw.githubusercontent.com/owner/repo/branch/path
		repo := strings.TrimSuffix(strings.TrimPrefix(repoURL, "https://github.com/"), ".git")
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", repo, branch, filePath), nil
	case strings.Contains(repoURL, "gitlab.com") || strings.Contains(repoURL, "gitlab"):
		// https://gitlab.com/owner/repo  →  https://gitlab.com/owner/repo/-/raw/branch/path
		base := strings.TrimSuffix(repoURL, ".git")
		return fmt.Sprintf("%s/-/raw/%s/%s", base, branch, filePath), nil
	default:
		return "", fmt.Errorf("unsupported git host for file-only fetch: %s", repoURL)
	}
}

// cloneRepo does a shallow clone into a temp directory and returns the path.
func cloneRepo(ctx context.Context, repoURL, branch, token string) (string, error) {
	dir, err := os.MkdirTemp("", "meshploy-stack-*")
	if err != nil {
		return "", err
	}
	cloneURL := repoURL
	if token != "" {
		// Embed token in URL for HTTPS auth.
		cloneURL = strings.Replace(repoURL, "https://", "https://x-access-token:"+token+"@", 1)
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", branch, cloneURL, dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("git clone: %w — %s", err, strings.TrimSpace(string(out)))
	}
	return dir, nil
}

// readFileFromClone reads a file from a cloned repo directory.
func readFileFromClone(dir, filePath string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, filepath.Clean(filePath)))
}

// headSHA returns the HEAD commit SHA from a cloned repo directory.
func headSHA(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func strMapToJSONObj(m map[string]string) meshdb.JSONObject {
	obj := make(meshdb.JSONObject, len(m))
	for k, v := range m {
		obj[k] = v
	}
	return obj
}

func jsonObjToStrMap(obj meshdb.JSONObject) map[string]string {
	m := make(map[string]string, len(obj))
	for k, v := range obj {
		if sv, ok := v.(string); ok {
			m[k] = sv
		}
	}
	return m
}
