package service

import (
	"log"

	"github.com/meshploy/apps/api/internal/config"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
)

type Services struct {
	Auth            *AuthService
	Orgs            *OrgService
	Projects        *ProjectService
	Nodes           *NodeService
	Workloads       *WorkloadService
	Domains         *DomainService
	Routes          *RouteService
	Deployments     *DeploymentService
	GitIntegrations *GitIntegrationService
	Headscale       *HeadscaleService  // nil if HEADSCALE_URL / HEADSCALE_API_KEY not set
	K8s             kubernetes.Interface // nil if KUBECONFIG unavailable
}

func New(db *gorm.DB, cfg ...*config.Config) *Services {
	var c *config.Config
	if len(cfg) > 0 {
		c = cfg[0]
	}

	// K8s client is optional — log a warning if not available.
	var k8sClient kubernetes.Interface
	if c != nil {
		var err error
		k8sClient, err = appk8s.NewClient(c.KubeconfigPath)
		if err != nil {
			log.Printf("warning: K8s not available (%v) — build/deploy features disabled", err)
		}
	}

	gitSvc := &GitIntegrationService{db: db, cfg: c}

	var headscaleSvc *HeadscaleService
	if c != nil && c.HeadscaleURL != "" && c.HeadscaleKey != "" {
		headscaleSvc = NewHeadscaleService(c.HeadscaleURL, c.HeadscaleKey)
	}

	return &Services{
		Auth:            &AuthService{db: db},
		Orgs:            &OrgService{db: db},
		Projects:        &ProjectService{db: db},
		Nodes:           &NodeService{db: db},
		Workloads:       &WorkloadService{db: db},
		Domains:         &DomainService{db: db},
		Routes:          &RouteService{db: db},
		Deployments:     &DeploymentService{db: db, cfg: c, k8s: k8sClient, git: gitSvc},
		GitIntegrations: gitSvc,
		Headscale:       headscaleSvc,
		K8s:             k8sClient,
	}
}
