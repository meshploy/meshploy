package mcpserver

import (
	"fmt"

	"github.com/meshploy/packages/client"
)

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
	// Published is false for a paused route, which is kept and not served.
	Published bool `json:"published"`
	// CustomHostname is true when the hostname is its own, not a subdomain of a
	// base domain. Every route create_route makes is one.
	CustomHostname bool `json:"custom_hostname"`
	// OwnershipVerified is whether a custom hostname has been proved. Until it
	// is, no certificate is issued and requests fail TLS even once its DNS
	// points at the gateway - so a route can look created and still not work.
	OwnershipVerified bool `json:"ownership_verified"`
	// ProveOwnership is present only while a custom hostname is unproved: the
	// records to add at its DNS provider, after which verify_route_hostname.
	ProveOwnership []MCPDNSRecord `json:"prove_ownership,omitempty"`
}

// MCPDNSRecord is one record for the user to add at their DNS provider.
type MCPDNSRecord struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

// MCPTCPRoute is a published port as an agent sees it. Status matters here in
// a way it does not for a domain route: the listener lives in the gateway, so
// creating the route and the port actually opening are two different moments.
type MCPTCPRoute struct {
	ID          string   `json:"id"`
	GatewayPort int      `json:"gateway_port"`
	Target      string   `json:"target"`
	ServiceID   string   `json:"service_id,omitempty"`
	AllowedFrom []string `json:"allowed_from"`
	Status      string   `json:"status"`
	LastError   string   `json:"last_error,omitempty"`
}

func toMCPTCPRoute(r client.TCPRoute) MCPTCPRoute {
	out := MCPTCPRoute{
		ID:          r.ID,
		GatewayPort: r.GatewayPort,
		Target:      fmt.Sprintf("%s:%d", r.TargetIP, r.TargetPort),
		AllowedFrom: r.AllowedCIDRs,
		Status:      r.Status,
		LastError:   r.LastError,
	}
	if out.AllowedFrom == nil {
		out.AllowedFrom = []string{}
	}
	if r.ServiceID != nil {
		out.ServiceID = *r.ServiceID
	}
	return out
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
	ID        string `json:"id"`
	Domain    string `json:"domain"`
	Verified  bool   `json:"verified"`
	IsPrimary bool   `json:"is_primary"`
	// delegation or ondemand. It decides whether an internal route on this
	// domain can hold a publicly trusted certificate, which an agent creating a
	// route should know before it promises one.
	DNSMode string `json:"dns_mode,omitempty"`
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

// MCPConfigFile is a config file as an agent sees it.
//
// There is no Content field, and that is deliberate rather than an oversight:
// the API does not return file bodies, so an agent can read where a file mounts
// and which services use it without the credential inside it entering a
// transcript. Size stands in for the body when checking a write landed.
type MCPConfigFile struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	Size     int      `json:"size"`
	StackID  string   `json:"stack_id,omitempty"`
	Services []string `json:"services"`
}

// MCPTemplate is a one-click template as an agent sees it: enough to choose
// one and know what it will ask for. Variables are declarations only - a
// prompted value is supplied on deploy and a generated one is made by the
// server, and neither can be read back.
type MCPTemplate struct {
	ID          string                `json:"id"`
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Category    string                `json:"category"`
	Version     string                `json:"version"`
	Variables   []MCPTemplateVariable `json:"variables,omitempty"`
}

type MCPTemplateVariable struct {
	Key      string `json:"key"`
	Prompt   string `json:"prompt,omitempty"`
	Required bool   `json:"required,omitempty"`
	// Generate names how the server makes the value (a password, a subdomain)
	// when the caller supplies none.
	Generate string `json:"generate,omitempty"`
}

func toMCPTemplate(t client.Template) MCPTemplate {
	out := MCPTemplate{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Category:    t.Category,
		Version:     t.Version,
	}
	for _, v := range t.Variables {
		out.Variables = append(out.Variables, MCPTemplateVariable{
			Key: v.Key, Prompt: v.Prompt, Required: v.Required, Generate: v.Generate,
		})
	}
	return out
}
