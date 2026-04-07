package service

import (
	"context"
	"log"

	"github.com/google/uuid"
	"github.com/meshploy/apps/api/internal/config"
	appk8s "github.com/meshploy/apps/api/internal/k8s"
	meshdb "github.com/meshploy/packages/db"
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

	nodes := &NodeService{db: db}
	domains := &DomainService{db: db}
	auth := &AuthService{db: db}

	// Wire gateway seeding: if GATEWAY_HOSTNAME is set the first user to register
	// gets the gateway node and base domain pre-created for their org.
	if c != nil && c.GatewayHostname != "" {
		auth.onFirstRegistration = func(ctx context.Context, orgID uuid.UUID) {
			if _, err := nodes.Register(ctx, orgID, c.GatewayHostname, c.GatewayIP, meshdb.K3sRoleServer); err != nil {
				log.Printf("warning: seed gateway node: %v", err)
			}
			if err := domains.CreateSeeded(ctx, orgID, c.Domain); err != nil {
				log.Printf("warning: seed domain: %v", err)
			}
		}
	}

	return &Services{
		Auth:            auth,
		Orgs:            &OrgService{db: db},
		Projects:        &ProjectService{db: db},
		Nodes:           nodes,
		Workloads:       &WorkloadService{db: db},
		Domains:         domains,
		Routes:          &RouteService{db: db},
		Deployments:     &DeploymentService{db: db, cfg: c, k8s: k8sClient, git: gitSvc},
		GitIntegrations: gitSvc,
		Headscale:       headscaleSvc,
		K8s:             k8sClient,
	}
}
