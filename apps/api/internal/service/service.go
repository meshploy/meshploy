package service

import "gorm.io/gorm"

type Services struct {
	Auth        *AuthService
	Orgs        *OrgService
	Projects    *ProjectService
	Nodes       *NodeService
	Workloads   *WorkloadService
	Routes      *RouteService
	Deployments *DeploymentService
}

func New(db *gorm.DB) *Services {
	return &Services{
		Auth:        &AuthService{db: db},
		Orgs:        &OrgService{db: db},
		Projects:    &ProjectService{db: db},
		Nodes:       &NodeService{db: db},
		Workloads:   &WorkloadService{db: db},
		Routes:      &RouteService{db: db},
		Deployments: &DeploymentService{db: db},
	}
}
