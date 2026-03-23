package db

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// --- Enums ---

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

type ServiceStatus string

const (
	ServiceDeploying ServiceStatus = "deploying"
	ServiceRunning   ServiceStatus = "running"
	ServiceStopped   ServiceStatus = "stopped"
	ServiceFailed    ServiceStatus = "failed"
)

type DeploymentStatus string

const (
	DeploymentPending DeploymentStatus = "pending"
	DeploymentSuccess DeploymentStatus = "success"
	DeploymentFailed  DeploymentStatus = "failed"
)

type ResourceType string

const (
	ResourceService ResourceType = "service"
	ResourceRoute   ResourceType = "route"
)

// --- Base ---

type Base struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// --- Models ---

type User struct {
	Base
	Username string `gorm:"uniqueIndex;not null" json:"username"`
	Email    string `gorm:"uniqueIndex;not null" json:"email"`
	Password string `json:"-"`
}

type Organization struct {
	Base
	Name string `gorm:"not null"        json:"name"`
	Slug string `gorm:"uniqueIndex;not null" json:"slug"`

	Members  []OrganizationMember `gorm:"foreignKey:OrganizationID" json:"-"`
	Projects []Project            `gorm:"foreignKey:OrganizationID" json:"-"`
	Nodes    []Node               `gorm:"foreignKey:OrganizationID" json:"-"`
}

type OrganizationMember struct {
	Base
	OrganizationID uuid.UUID  `gorm:"type:uuid;not null;index"                        json:"organization_id"`
	UserID         uuid.UUID  `gorm:"type:uuid;not null;index"                        json:"user_id"`
	Role           MemberRole `gorm:"type:varchar(10);not null;default:'member'"      json:"role"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
	User         User         `gorm:"foreignKey:UserID"         json:"-"`
}

func (OrganizationMember) TableName() string { return "organization_members" }

type Project struct {
	Base
	OrganizationID uuid.UUID `gorm:"type:uuid;not null;index" json:"organization_id"`
	Name           string    `gorm:"not null"                 json:"name"`
	Slug           string    `gorm:"uniqueIndex;not null"     json:"slug"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
	Services     []Service    `gorm:"foreignKey:ProjectID"      json:"-"`
	Routes       []Route      `gorm:"foreignKey:ProjectID"      json:"-"`
}

type Node struct {
	Base
	OrganizationID uuid.UUID  `gorm:"type:uuid;not null;index" json:"organization_id"`
	Name           string     `gorm:"not null"                 json:"name"`
	TailscaleIP    string     `gorm:"not null"                 json:"tailscale_ip"`
	Status         NodeStatus `gorm:"type:varchar(10);not null;default:'offline'" json:"status"`
	LastSeenAt     *time.Time `json:"last_seen_at"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
}

type Service struct {
	Base
	ProjectID    uuid.UUID     `gorm:"type:uuid;not null;index" json:"project_id"`
	NodeID       uuid.UUID     `gorm:"type:uuid;not null;index" json:"node_id"`
	Name         string        `gorm:"not null"                 json:"name"`
	Image        string        `gorm:"not null"                 json:"image"`
	InternalPort int           `gorm:"not null"                 json:"internal_port"`
	EnvVars      EnvVarsMap    `gorm:"type:jsonb;default:'{}'"  json:"env_vars"`
	Status       ServiceStatus `gorm:"type:varchar(10);not null;default:'stopped'" json:"status"`

	Project     Project      `gorm:"foreignKey:ProjectID" json:"-"`
	Node        Node         `gorm:"foreignKey:NodeID"    json:"-"`
	Routes      []Route      `gorm:"foreignKey:ServiceID" json:"-"`
	Deployments []Deployment `gorm:"foreignKey:ServiceID" json:"-"`
}

type Route struct {
	Base
	OrganizationID uuid.UUID  `gorm:"type:uuid;not null;index"          json:"organization_id"`
	ProjectID      uuid.UUID  `gorm:"type:uuid;not null;index"          json:"project_id"`
	ServiceID      *uuid.UUID `gorm:"type:uuid;index"                   json:"service_id"` // nullable
	Hostname       string     `gorm:"uniqueIndex;not null"              json:"hostname"`
	TargetIP       string     `gorm:"not null"                          json:"target_ip"`
	TargetPort     int        `gorm:"not null"                          json:"target_port"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
	Project      Project      `gorm:"foreignKey:ProjectID"      json:"-"`
	Service      *Service     `gorm:"foreignKey:ServiceID"      json:"-"`
}

type ResourcePermission struct {
	Base
	OrganizationID uuid.UUID    `gorm:"type:uuid;not null;index" json:"organization_id"`
	UserID         uuid.UUID    `gorm:"type:uuid;not null;index" json:"user_id"`
	ResourceType   ResourceType `gorm:"type:varchar(10);not null" json:"resource_type"`
	ResourceID     uuid.UUID    `gorm:"type:uuid;not null"        json:"resource_id"`

	Organization Organization `gorm:"foreignKey:OrganizationID" json:"-"`
	User         User         `gorm:"foreignKey:UserID"         json:"-"`
}

func (ResourcePermission) TableName() string { return "resource_permissions" }

type Deployment struct {
	Base
	ServiceID  uuid.UUID        `gorm:"type:uuid;not null;index" json:"service_id"`
	Status     DeploymentStatus `gorm:"type:varchar(10);not null;default:'pending'" json:"status"`
	Log        string           `gorm:"type:text"                json:"log"`
	DeployedAt *time.Time       `json:"deployed_at"`

	Service Service `gorm:"foreignKey:ServiceID" json:"-"`
}
