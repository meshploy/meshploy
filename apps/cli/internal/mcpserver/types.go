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
