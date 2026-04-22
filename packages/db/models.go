package db

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

type MemberRole string

const (
	RoleOwner  MemberRole = "owner"
	RoleAdmin  MemberRole = "admin"
	RoleMember MemberRole = "member"
)

type NodeStatus string

const (
	NodeOnline  NodeStatus = "online"
	NodeOffline NodeStatus = "offline"
)

type K3sRole string

const (
	K3sRoleServer K3sRole = "server"
	K3sRoleAgent  K3sRole = "agent"
)

// MeshRole controls which workloads are scheduled on a node via k8s labels/taints.
type MeshRole string

const (
	// MeshRoleWorkloadBuilder (default) — accepts both customer workloads and build jobs.
	MeshRoleWorkloadBuilder MeshRole = "workload_builder"
	// MeshRoleWorkload — customer workloads only, no build jobs.
	MeshRoleWorkload MeshRole = "workload"
	// MeshRoleBuilder — build jobs only; tainted so customer workloads can't land here.
	MeshRoleBuilder MeshRole = "builder"
)

type ServiceType string

const (
	ServiceTypeApplication ServiceType = "application"
	ServiceTypeDatabase    ServiceType = "database"
)

type ServiceStatus string

const (
	ServiceDeploying ServiceStatus = "deploying"
	ServiceRunning   ServiceStatus = "running"
	ServiceStopped   ServiceStatus = "stopped"
	ServiceFailed    ServiceStatus = "failed"
)

type BuilderType string

const (
	BuilderNixpacks   BuilderType = "nixpacks"
	BuilderRailpack   BuilderType = "railpack"
	BuilderDockerfile BuilderType = "dockerfile"
	BuilderImage      BuilderType = "image"
)

type DatabaseEngine string

const (
	DatabasePostgres DatabaseEngine = "postgres"
	DatabaseMySQL    DatabaseEngine = "mysql"
	DatabaseRedis    DatabaseEngine = "redis"
	DatabaseMongoDB  DatabaseEngine = "mongodb"
)

type DeploymentStatus string

const (
	DeploymentPending   DeploymentStatus = "pending"
	DeploymentBuilding  DeploymentStatus = "building"
	DeploymentDeploying DeploymentStatus = "deploying"
	DeploymentRunning   DeploymentStatus = "running"
	DeploymentSuccess   DeploymentStatus = "success"
	DeploymentFailed    DeploymentStatus = "failed"
)

type BackupStatus string

const (
	BackupPending BackupStatus = "pending"
	BackupRunning BackupStatus = "running"
	BackupSuccess BackupStatus = "success"
	BackupFailed  BackupStatus = "failed"
)

type NotificationChannelType string

const (
	NotificationSlack   NotificationChannelType = "slack"
	NotificationDiscord NotificationChannelType = "discord"
	NotificationEmail   NotificationChannelType = "email"
	NotificationWebhook NotificationChannelType = "webhook"
)

type StorageProvider string

const (
	StorageS3    StorageProvider = "s3"
	StorageR2    StorageProvider = "r2"
	StorageMinio StorageProvider = "minio"
	StorageB2    StorageProvider = "b2"
)

type RegistryProvider string

const (
	RegistryGHCR      RegistryProvider = "ghcr"
	RegistryDockerHub RegistryProvider = "dockerhub"
	RegistryECR       RegistryProvider = "ecr"
	RegistryGCR       RegistryProvider = "gcr"
	RegistryCustom    RegistryProvider = "custom"
	RegistryBuiltin   RegistryProvider = "builtin" // self-hosted registry:2 on gateway
)

type TemplateCategory string

const (
	TemplateCategoryDatabase    TemplateCategory = "database"
	TemplateCategoryCMS         TemplateCategory = "cms"
	TemplateCategoryAnalytics   TemplateCategory = "analytics"
	TemplateCategoryQueue       TemplateCategory = "queue"
	TemplateCategoryMonitoring  TemplateCategory = "monitoring"
	TemplateCategoryApplication TemplateCategory = "application"
)

type ResourceType string

const (
	ResourceService ResourceType = "service"
	ResourceRoute   ResourceType = "route"
)

type RouteZone string

const (
	RouteZonePublic   RouteZone = "public"
	RouteZoneInternal RouteZone = "internal"
	RouteZonePreview  RouteZone = "preview"
)

// ---------------------------------------------------------------------------
// Base
// ---------------------------------------------------------------------------

type Base struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index"                                          json:"-"`
}

// ---------------------------------------------------------------------------
// Identity & Access
// ---------------------------------------------------------------------------

type User struct {
	Base
	Username string `gorm:"uniqueIndex;not null" json:"username"`
	Email    string `gorm:"uniqueIndex;not null" json:"email"`
	Password string `json:"-"`
}

type Organization struct {
	Base
	Name string `gorm:"not null"             json:"name"`
	Slug string `gorm:"uniqueIndex;not null" json:"slug"`

	// Headscale preauth key — persisted encrypted so it survives page navigation.
	// Populated by CreateHeadscalePreAuthKey, auto-cleared on expiry by GetHeadscalePreAuthKey.
	HeadscalePreAuthKey       EncryptedString `gorm:"type:text" json:"-"`
	HeadscalePreAuthKeyExpiry *time.Time      `                 json:"-"`

	Members  []OrganizationMember `gorm:"foreignKey:OrganizationID" json:"-"`
	Projects []Project            `gorm:"foreignKey:OrganizationID" json:"-"`
	Nodes    []Node               `gorm:"foreignKey:OrganizationID" json:"-"`
}

type OrganizationMember struct {
	Base
	OrganizationID uuid.UUID  `gorm:"type:uuid;not null;index"                   json:"organization_id"`
	UserID         uuid.UUID  `gorm:"type:uuid;not null;index"                   json:"user_id"`
	Role           MemberRole `gorm:"type:varchar(10);not null;default:'member'" json:"role"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
	User         User         `gorm:"foreignKey:UserID"         json:"-"`
}

func (OrganizationMember) TableName() string { return "organization_members" }

type ResourcePermission struct {
	Base
	OrganizationID uuid.UUID    `gorm:"type:uuid;not null;index"  json:"organization_id"`
	UserID         uuid.UUID    `gorm:"type:uuid;not null;index"  json:"user_id"`
	ResourceType   ResourceType `gorm:"type:varchar(10);not null" json:"resource_type"`
	ResourceID     uuid.UUID    `gorm:"type:uuid;not null"        json:"resource_id"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
	User         User         `gorm:"foreignKey:UserID"         json:"-"`
}

func (ResourcePermission) TableName() string { return "resource_permissions" }

// ---------------------------------------------------------------------------
// Projects & Infrastructure
// ---------------------------------------------------------------------------

type Project struct {
	Base
	OrganizationID uuid.UUID `gorm:"type:uuid;not null;index" json:"organization_id"`
	Name           string    `gorm:"not null"                 json:"name"`
	// Slug doubles as the K8s namespace name for this project.
	// Enforced pattern: ^[a-z0-9-]+$ (matches K8s namespace constraints).
	Slug string `gorm:"uniqueIndex;not null" json:"slug"`

	// Project-level env vars shared across all services (AES-256-GCM encrypted .env block).
	// Service-level env vars override these when the same key appears.
	EnvVars EncryptedString `gorm:"type:text" json:"-"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
	Services     []Service    `gorm:"foreignKey:ProjectID"      json:"-"`
	Routes       []Route      `gorm:"foreignKey:ProjectID"      json:"-"`
}

type Node struct {
	Base
	OrganizationID uuid.UUID  `gorm:"type:uuid;not null;index"                    json:"organization_id"`
	Name           string     `gorm:"not null"                                    json:"name"`
	TailscaleIP    string     `gorm:"not null"                                    json:"tailscale_ip"`
	HeadscaleID    string     `gorm:"default:''"                                  json:"headscale_id"` // Headscale peer ID — stable across IP changes
	Status         NodeStatus `gorm:"type:varchar(10);not null;default:'offline'" json:"status"`
	LastSeenAt     *time.Time `json:"last_seen_at"`

	// K3s
	K3sRole    K3sRole    `gorm:"type:varchar(10);not null;default:'agent'"    json:"k3s_role"`
	K3sVersion string     `json:"k3s_version"` // e.g. "v1.28.4+k3s1"
	K3sLabels  JSONObject `gorm:"type:jsonb;default:'{}'"                      json:"k3s_labels"`
	// e.g. {"meshploy.com/role": "builder", "topology.kubernetes.io/region": "us-east"}
	MeshRole MeshRole `gorm:"type:varchar(20);not null;default:''" json:"mesh_role"`
	// workload_builder | workload | builder — controls k8s labels/taints applied to this node

	// Public IP — set on gateway (server) nodes only; used for DNS instructions.
	PublicIP string `gorm:"not null;default:''" json:"public_ip"`

	// Capacity — populated by node agent heartbeat
	CPUCores float32 `json:"cpu_cores"`
	MemoryGB float32 `json:"memory_gb"`
	DiskGB   float32 `json:"disk_gb"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
}

// ---------------------------------------------------------------------------
// Workloads  (unified polymorphic table)
// ---------------------------------------------------------------------------

type Service struct {
	Base
	ProjectID uuid.UUID   `gorm:"type:uuid;not null;index"                     json:"project_id"`
	NodeID    *uuid.UUID  `gorm:"type:uuid;index"                              json:"node_id"` // nullable = let K3s schedule
	Name      string      `gorm:"not null"                                     json:"name"`
	Type      ServiceType `gorm:"type:varchar(15);not null;default:'application'" json:"type"`

	// Runtime image.
	// - application/image builder: set at creation.
	// - application/other builders: populated after first successful build.
	// - database: set at creation (e.g. "postgres:15").
	Image string `json:"image"`

	Status   ServiceStatus `gorm:"type:varchar(10);not null;default:'stopped'" json:"status"`
	Replicas int           `gorm:"not null;default:1"                          json:"replicas"`

	// K8s resource spec (standard K8s quantity strings)
	CPURequest    string `gorm:"not null;default:'100m'"  json:"cpu_request"`
	CPULimit      string `gorm:"not null;default:'500m'"  json:"cpu_limit"`
	MemoryRequest string `gorm:"not null;default:'128Mi'" json:"memory_request"`
	MemoryLimit   string `gorm:"not null;default:'512Mi'" json:"memory_limit"`

	// Service-level env vars (AES-256-GCM encrypted .env block).
	// Merged with project-level env vars at deploy time; service keys win on conflict.
	EnvVars EncryptedString `gorm:"type:text" json:"-"`

	Project        Project         `gorm:"foreignKey:ProjectID"  json:"-"`
	Node           *Node           `gorm:"foreignKey:NodeID"     json:"-"`
	BuildConfig    *BuildConfig    `gorm:"foreignKey:ServiceID"  json:"-"`
	DatabaseConfig *DatabaseConfig `gorm:"foreignKey:ServiceID"  json:"-"`
	Routes         []Route         `gorm:"foreignKey:ServiceID"  json:"-"`
	Deployments    []Deployment    `gorm:"foreignKey:ServiceID"  json:"-"`
}

// BuildConfig holds app-specific build settings. 1:1 with Service (type=application).
type BuildConfig struct {
	Base
	ServiceID uuid.UUID   `gorm:"type:uuid;not null;uniqueIndex" json:"service_id"`
	Builder   BuilderType `gorm:"type:varchar(15);not null"      json:"builder"`

	// Git source
	GitIntegrationID *uuid.UUID `gorm:"type:uuid"              json:"git_integration_id"`
	GitRepo           string    `json:"git_repo"`
	Branch            string    `gorm:"default:'main'"         json:"branch"`
	RootDir  string `gorm:"default:'.'"`           // root of the app within the repo
	// Dockerfile builder
	DockerfilePath string     `gorm:"default:'Dockerfile'" json:"dockerfile_path"`
	BuildArgs      EnvVarsMap `gorm:"type:jsonb;default:'{}'" json:"build_args"`

	// Build-time environment variables — KEY=VALUE, one per line.
	// Passed to nixpacks (--env), railpack (export), or dockerfile (--build-arg).
	// Encrypted at rest; accessed via GET .../build-config/env-vars.
	BuildEnvVars EncryptedString `gorm:"type:text" json:"-"`

	// BuilderNode pins builds to a specific K8s node (k8s_node_name).
	// Empty string = any node with meshploy.com/role=builder (default).
	BuilderNode string `gorm:"not null;default:''" json:"builder_node"`

	// Resource requests for the build job pod.
	// Empty = use service layer defaults (1000m CPU, 1Gi memory).
	BuilderCPURequest    string `gorm:"not null;default:''" json:"builder_cpu_request"`
	BuilderMemoryRequest string `gorm:"not null;default:''" json:"builder_memory_request"`

	// Registry to push the built image to.
	// nil = use the internal mesh registry (default, zero-config CE experience).
	RegistryIntegrationID *uuid.UUID `gorm:"type:uuid" json:"registry_integration_id"`

	// Populated after a successful build — used for the next deployment.
	LastBuiltImage string     `json:"last_built_image"`
	LastBuiltAt    *time.Time `json:"last_built_at"`

	Service             Service              `gorm:"foreignKey:ServiceID"             json:"-"`
	RegistryIntegration *RegistryIntegration `gorm:"foreignKey:RegistryIntegrationID" json:"-"`
}

// DatabaseConfig holds managed-database settings. 1:1 with Service (type=database).
type DatabaseConfig struct {
	Base
	ServiceID uuid.UUID      `gorm:"type:uuid;not null;uniqueIndex" json:"service_id"`
	Engine    DatabaseEngine `gorm:"type:varchar(10);not null"      json:"engine"`
	Version   string         `gorm:"not null"                       json:"version"` // e.g. "15", "8.0", "7"
	StorageGB int            `gorm:"not null;default:10"            json:"storage_gb"`

	// Auto-generated connection string stored encrypted directly on this record.
	// Meshploy populates this when the database is provisioned.
	ConnectionString EncryptedString `gorm:"type:text" json:"-"`

	Service Service `gorm:"foreignKey:ServiceID" json:"-"`
}

// NodeRegistrationToken is a long-lived pre-shared token that allows a worker
// node to self-register into the org's node list without a user JWT.
// There is at most one token per organisation (unique index on OrganizationID).
// Regenerating replaces the existing row via upsert.
type NodeRegistrationToken struct {
	Base
	OrganizationID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"organization_id"`
	Token          string    `gorm:"not null"                       json:"token"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
}

func (NodeRegistrationToken) TableName() string { return "node_registration_tokens" }

// ---------------------------------------------------------------------------
// Domains
// ---------------------------------------------------------------------------

// Domain represents an org-owned base domain managed by the org's CoreDNS + Caddy.
// CE limit: 1 domain per org (enforced in service layer). EE removes this limit.
//
// base_domain is immutable once set — users must add a new domain and delete the old one.
// internal_subdomain and preview_subdomain are mutable (wildcard TLS makes renames cheap).
type Domain struct {
	Base
	OrganizationID uuid.UUID `gorm:"type:uuid;not null;index"         json:"organization_id"`
	// Immutable once set. Global unique index prevents cross-org domain hijacking
	// (combined with ownership verification via DNS TXT record).
	BaseDomain        string `gorm:"uniqueIndex;not null"             json:"base_domain"`
	InternalSubdomain string `gorm:"not null;default:'internal'"      json:"internal_subdomain"`
	PreviewSubdomain  string `gorm:"not null;default:'preview'"       json:"preview_subdomain"`
	// Pending domains cannot be used for routing until verified.
	Verified bool `gorm:"default:false" json:"verified"`
	// DNS TXT record value for ownership proof:
	//   _meshploy-verify.{base_domain}  TXT  {verify_token}
	VerifyToken string `gorm:"not null" json:"-"`

	Organization Organization `gorm:"foreignKey:OrganizationID"                        json:"-"`
	// RESTRICT: domain cannot be deleted while routes reference it.
	Routes []Route `gorm:"foreignKey:DomainID;constraint:OnDelete:RESTRICT" json:"-"`
}

// ---------------------------------------------------------------------------
// Traffic
// ---------------------------------------------------------------------------

type Route struct {
	Base
	OrganizationID uuid.UUID  `gorm:"type:uuid;not null;index"  json:"organization_id"`
	ProjectID      uuid.UUID  `gorm:"type:uuid;not null;index"  json:"project_id"`
	ServiceID      *uuid.UUID `gorm:"type:uuid;index"           json:"service_id"` // nullable — loose coupling
	// DomainID links to the managed Domain. Nullable for manually-specified hostnames.
	DomainID  *uuid.UUID `gorm:"type:uuid;index"           json:"domain_id"`
	Zone      RouteZone  `gorm:"type:varchar(10)"           json:"zone"`      // public|internal|preview
	Subdomain string     `json:"subdomain"`                                   // prefix, e.g. "keeper"
	Hostname  string     `gorm:"uniqueIndex;not null"       json:"hostname"`  // hot-path proxy lookup (denormalised)
	TargetIP  string     `gorm:"not null"                   json:"target_ip"` // Headscale mesh IP
	TargetPort int       `gorm:"not null"                   json:"target_port"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
	Project      Project      `gorm:"foreignKey:ProjectID"      json:"-"`
	Service      *Service     `gorm:"foreignKey:ServiceID"      json:"-"`
	Domain       *Domain      `gorm:"foreignKey:DomainID"       json:"-"`
}

// ---------------------------------------------------------------------------
// Deployment History
// ---------------------------------------------------------------------------

type Deployment struct {
	Base
	ServiceID uuid.UUID        `gorm:"type:uuid;not null;index"                    json:"service_id"`
	Status    DeploymentStatus `gorm:"type:varchar(10);not null;default:'pending'" json:"status"`
	Image     string           `json:"image"` // image used for this specific deployment

	// K8s artefacts — stored for auditing and rollback
	AppliedManifest string `gorm:"type:text" json:"applied_manifest"` // K8s YAML applied
	BuildJobName    string `json:"build_job_name"`                    // K8s Job name for the build

	Log        string     `gorm:"type:text" json:"log"`
	DeployedAt *time.Time `json:"deployed_at"`

	Service Service `gorm:"foreignKey:ServiceID" json:"-"`
}

// ---------------------------------------------------------------------------
// Integrations
// ---------------------------------------------------------------------------

// StorageIntegration holds org-level S3-compatible credentials for backups.
// Credentials are encrypted at rest via EncryptedString.
type StorageIntegration struct {
	Base
	OrganizationID  uuid.UUID       `gorm:"type:uuid;not null;index" json:"organization_id"`
	Name            string          `gorm:"not null"                 json:"name"` // user-given label
	Provider        StorageProvider `gorm:"type:varchar(10);not null" json:"provider"`
	Endpoint        string          `json:"endpoint"` // S3-compatible endpoint URL
	Region          string          `json:"region"`
	Bucket          string          `gorm:"not null"           json:"bucket"`
	AccessKeyID     EncryptedString `gorm:"type:text;not null" json:"-"`
	SecretAccessKey EncryptedString `gorm:"type:text;not null" json:"-"`

	Organization Organization  `gorm:"foreignKey:OrganizationID" json:"-"`
	BackupConfigs []BackupConfig `gorm:"foreignKey:StorageIntegrationID" json:"-"`
}

func (StorageIntegration) TableName() string { return "storage_integrations" }

// RegistryIntegration holds org-level container registry credentials.
// nil RegistryIntegrationID on BuildConfig = use internal mesh registry.
type RegistryIntegration struct {
	Base
	OrganizationID uuid.UUID        `gorm:"type:uuid;not null;index"  json:"organization_id"`
	Name           string           `gorm:"not null"                  json:"name"`
	Provider       RegistryProvider `gorm:"type:varchar(15);not null" json:"provider"`
	Endpoint       string           `json:"endpoint"`   // custom registry URL
	Namespace      string           `json:"namespace"`  // e.g. "ghcr.io/myorg"
	Username       EncryptedString  `gorm:"type:text;not null" json:"-"`
	Password       EncryptedString  `gorm:"type:text;not null" json:"-"` // token for GHCR/ECR

	Organization Organization  `gorm:"foreignKey:OrganizationID" json:"-"`
}

func (RegistryIntegration) TableName() string { return "registry_integrations" }

// GitIntegration holds an org-level connection to a Git hosting provider.
// GitHub: each integration is its own GitHub App (manifest flow). Two phases:
//   - Pending (Connected=false): app created on GitHub, InstallationID not yet set
//   - Connected (Connected=true): app installed on a GitHub account/org, InstallationID set
//
// GitLab/Gitea support two auth methods:
//   - PAT:   InstallationID = personal access token (encrypted)
//   - OAuth: OAuthClientID + OAuthClientSecret = OAuth App credentials;
//            InstallationID = OAuth access token (populated after callback)
type GitIntegration struct {
	Base
	OrganizationID    uuid.UUID       `gorm:"type:uuid;not null;index"   json:"organization_id"`
	Provider          string          `gorm:"type:varchar(15);not null"  json:"provider"`          // "github" | "gitlab" | "gitea"
	AuthMethod        string          `gorm:"not null;default:'pat'"     json:"auth_method"`       // "app" | "pat" | "oauth"
	Name              string          `gorm:"not null"                   json:"name"`
	InstallationID    EncryptedString `gorm:"type:text"                  json:"-"`                 // GitHub: installation_id; GitLab/Gitea PAT: token; GitLab/Gitea OAuth: access token
	BaseURL           string          `gorm:"not null;default:''"        json:"base_url"`          // empty = hosted (github.com / gitlab.com); self-hosted URL otherwise
	OAuthClientID     string          `gorm:"not null;default:''"        json:"-"`                 // GitLab/Gitea OAuth App client_id
	OAuthClientSecret EncryptedString `gorm:"type:text"                  json:"-"`                 // GitLab/Gitea OAuth App client_secret
	OAuthRedirectURI  string          `gorm:"not null;default:''"        json:"-"`                 // redirect_uri used when initiating the OAuth flow — must match callback
	OAuthRefreshToken EncryptedString `gorm:"type:text"                  json:"-"`                 // GitLab/Gitea OAuth refresh token (used to renew expired access tokens)
	OAuthTokenExpiry  *time.Time      `gorm:"default:null"               json:"-"`                 // when the current access token expires; nil = unknown / PAT
	Groups            string          `gorm:"not null;default:''"        json:"groups,omitempty"` // GitLab: group path; Gitea: org name — scopes repo listing

	// GitHub App credentials (auth_method="app" only). All encrypted at rest.
	GHAppID         string          `gorm:"not null;default:''" json:"-"`
	GHAppSlug       string          `gorm:"not null;default:''" json:"gh_app_slug,omitempty"` // exposed for pending card display
	GHClientID      string          `gorm:"not null;default:''" json:"-"`
	GHClientSecret  EncryptedString `gorm:"type:text"           json:"-"`
	GHPrivateKey    EncryptedString `gorm:"type:text"           json:"-"`
	GHWebhookSecret EncryptedString `gorm:"type:text"           json:"-"`

	// Connected is computed (not stored): true when InstallationID is non-empty.
	// Set by the service layer after loading from DB.
	Connected bool `gorm:"-" json:"connected"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
}

func (GitIntegration) TableName() string { return "git_integrations" }

// ---------------------------------------------------------------------------
// Operations
// ---------------------------------------------------------------------------

// BackupConfig defines an automated backup schedule for a Service.
// Backup execution targets the StorageIntegration's S3-compatible bucket.
type BackupConfig struct {
	Base
	ServiceID             uuid.UUID    `gorm:"type:uuid;not null;index" json:"service_id"`
	StorageIntegrationID  uuid.UUID    `gorm:"type:uuid;not null"       json:"storage_integration_id"`
	Schedule              string       `gorm:"not null"                 json:"schedule"`       // cron: "0 2 * * *"
	RetentionDays         int          `gorm:"not null;default:30"      json:"retention_days"`
	PathPrefix            string       `json:"path_prefix"` // S3 key prefix for this service
	Enabled               bool         `gorm:"default:true"             json:"enabled"`
	LastBackupAt          *time.Time   `json:"last_backup_at"`
	LastBackupStatus      *BackupStatus `json:"last_backup_status"`

	Service            Service            `gorm:"foreignKey:ServiceID"            json:"-"`
	StorageIntegration StorageIntegration `gorm:"foreignKey:StorageIntegrationID" json:"-"`
}

func (BackupConfig) TableName() string { return "backup_configs" }

// NotificationChannel defines a webhook/email destination for org-level events.
// Config JSONB schema depends on Type:
//   slack/discord: {"webhook_url": "https://..."}
//   email:         {"address": "ops@company.com"}
//   webhook:       {"url": "https://...", "secret": "..."}
//
// Events JSONB is an array of event strings:
//   "deploy.success" | "deploy.failed" | "node.offline" |
//   "backup.success" | "backup.failed"
type NotificationChannel struct {
	Base
	OrganizationID uuid.UUID               `gorm:"type:uuid;not null;index"  json:"organization_id"`
	Name           string                  `gorm:"not null"                  json:"name"`
	Type           NotificationChannelType `gorm:"type:varchar(10);not null" json:"type"`
	Config         JSONObject              `gorm:"type:jsonb;not null;default:'{}'" json:"config"`
	Events         StringArray             `gorm:"type:jsonb;not null;default:'[]'" json:"events"`
	Enabled        bool                    `gorm:"default:true"              json:"enabled"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
}

func (NotificationChannel) TableName() string { return "notification_channels" }

// ---------------------------------------------------------------------------
// Templates
// ---------------------------------------------------------------------------

// Template represents a 1-click deployment blueprint.
// Official templates (is_official=true) are shipped by Meshploy; OrganizationID is null.
// User templates (is_official=false) are private to an org.
//
// Manifest JSONB describes the services/configs to instantiate — think of it
// as a Meshploy-flavoured Compose/Helm values file resolved by the API at
// deploy time.
type Template struct {
	Base
	OrganizationID *uuid.UUID       `gorm:"type:uuid;index"           json:"organization_id"` // null = official
	Name           string           `gorm:"not null"                  json:"name"`
	Description    string           `json:"description"`
	Category       TemplateCategory `gorm:"type:varchar(15);not null" json:"category"`
	IconURL        string           `json:"icon_url"`
	Manifest       JSONObject       `gorm:"type:jsonb;not null;default:'{}'" json:"manifest"`
	DefaultEnvVars EnvVarsMap       `gorm:"type:jsonb;default:'{}'"          json:"default_env_vars"`
	IsOfficial     bool             `gorm:"default:false"             json:"is_official"`
	// Official-only: points to the public manifest registry entry
	SourceURL string `json:"source_url"`
	Version   string `json:"version"`

	Organization *Organization `gorm:"foreignKey:OrganizationID" json:"-"`
}
