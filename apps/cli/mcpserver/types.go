package mcpserver

// Lean projection types — only fields Claude needs. Metadata (created_at,
// updated_at, slug, project_id, k8s_name) is intentionally excluded.

type MCPService struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Image  string `json:"image"`
}

type MCPJob struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule,omitempty"`
	Status    string `json:"status"`
	LastRunAt string `json:"last_run_at,omitempty"`
}

type MCPMount struct {
	ID        string `json:"id"`
	ServiceID string `json:"service_id"`
	MountPath string `json:"mount_path"`
}

type MCPVolume struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	StorageGB int        `json:"storage_gb"`
	Status    string     `json:"status"`
	Mounts    []MCPMount `json:"mounts,omitempty"`
}

type MCPStack struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Spec   string `json:"spec,omitempty"`
}

type MCPRoute struct {
	ID        string `json:"id"`
	Hostname  string `json:"hostname"`
	ServiceID string `json:"service_id,omitempty"`
	Port      int    `json:"port"`
}

type MCPProject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type MCPNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Status string `json:"status"`
	Role   string `json:"role"`
}

type MCPSecret struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MCPDeployment struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Image      string `json:"image,omitempty"`
	DeployedAt string `json:"deployed_at,omitempty"`
}

type MCPJobRun struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at,omitempty"`
	FinishedAt string `json:"finished_at,omitempty"`
}

type MCPBuildConfig struct {
	Builder        string `json:"builder"`
	GitRepo        string `json:"git_repo,omitempty"`
	Branch         string `json:"branch,omitempty"`
	DockerfilePath string `json:"dockerfile_path,omitempty"`
	AutoDeploy     bool   `json:"auto_deploy"`
}

type MCPVariableGroupItem struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	IsSecret bool   `json:"is_secret"`
}

type MCPVariableGroup struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Items       []MCPVariableGroupItem `json:"items,omitempty"`
}

type MCPMember struct {
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_email"`
}

type MCPInvitation struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	ExpiresAt string `json:"expires_at"`
}

type MCPPermission struct {
	ID           string `json:"id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Action       string `json:"action"`
	ResourceName string `json:"resource_name,omitempty"`
}

type MCPPermissionsWithUser struct {
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_email"`
	Action    string `json:"action"`
}

type MCPBackupConfig struct {
	ID                   string `json:"id"`
	StorageIntegrationID string `json:"storage_integration_id"`
	Schedule             string `json:"schedule"`
	RetentionDays        int    `json:"retention_days"`
	Enabled              bool   `json:"enabled"`
}

type MCPBackupObject struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
}

type MCPNotificationChannel struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Enabled bool     `json:"enabled"`
	Events  []string `json:"events,omitempty"`
}

type MCPDomain struct {
	ID       string `json:"id"`
	Domain   string `json:"domain"`
	Verified bool   `json:"verified"`
}

type MCPRegistrationToken struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type MCPProvisioningToken struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Token     string `json:"token,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type MCPPod struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Ready     bool   `json:"ready"`
	Restarts  int32  `json:"restarts"`
	NodeName  string `json:"node_name"`
	StartedAt string `json:"started_at,omitempty"`
}

type MCPDatabaseConfig struct {
	Engine    string `json:"engine"`
	Version   string `json:"version"`
	StorageGB int    `json:"storage_gb"`
	DBName    string `json:"db_name"`
	DBUser    string `json:"db_user"`
}

type MCPQueryResult struct {
	Columns []string `json:"columns"`
	Rows    []any    `json:"rows"`
	Count   int      `json:"count"`
}

type MCPGitIntegration struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	AuthMethod string `json:"auth_method"`
	Connected  bool   `json:"connected"`
}

type MCPRegistryIntegration struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Namespace string `json:"namespace,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
}

type MCPStorageIntegration struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Endpoint string `json:"endpoint,omitempty"`
	Region   string `json:"region,omitempty"`
	Bucket   string `json:"bucket"`
}
