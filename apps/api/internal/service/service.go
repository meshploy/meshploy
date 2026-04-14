package service

import (
	"context"
	"log"
	"time"

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
	Registries      *RegistryService
	Headscale       *HeadscaleService   // nil if HEADSCALE_URL / HEADSCALE_API_KEY not set
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
		k8sClient, err = appk8s.NewClient(c.KubeconfigPath, c.K3sServerURL)
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
	registries := &RegistryService{db: db}

	// seedBuiltinRegistry ensures a builtin registry row exists for the org when
	// BUILTIN_REGISTRY_ENDPOINT is configured.
	seedBuiltinRegistry := func(ctx context.Context, orgID uuid.UUID) {
		if c == nil || c.BuiltinRegistryEndpoint == "" {
			return
		}
		if err := registries.SeedBuiltin(ctx, orgID, c.BuiltinRegistryEndpoint); err != nil {
			log.Printf("warning: seed builtin registry: %v", err)
		}
	}

	// seedGateway creates the gateway node and domain for an org if not already present.
	seedGateway := func(ctx context.Context, orgID uuid.UUID) {
		var nodeCount int64
		if db.WithContext(ctx).Model(&meshdb.Node{}).
			Where("organization_id = ? AND tailscale_ip = ?", orgID, c.GatewayIP).
			Count(&nodeCount).Error == nil && nodeCount == 0 {
			if _, err := nodes.Register(ctx, orgID, c.GatewayHostname, c.GatewayIP, meshdb.K3sRoleServer); err != nil {
				log.Printf("warning: seed gateway node: %v", err)
			}
		}
		if err := domains.CreateSeeded(ctx, orgID, c.Domain); err != nil {
			log.Printf("warning: seed domain: %v", err)
		}
	}

	// Wire gateway seeding: if GATEWAY_HOSTNAME is set, seed on first registration
	// and also seed any existing orgs on startup (handles pre-existing installations).
	if c != nil && c.GatewayHostname != "" {
		auth.onFirstRegistration = func(ctx context.Context, orgID uuid.UUID) {
			seedGateway(ctx, orgID)
			seedBuiltinRegistry(ctx, orgID)
		}

		// Seed existing orgs on startup.
		go func() {
			time.Sleep(2 * time.Second) // let DB connections settle
			ctx := context.Background()
			var orgs []meshdb.Organization
			if err := db.WithContext(ctx).Find(&orgs).Error; err != nil || len(orgs) == 0 {
				return
			}
			for _, org := range orgs {
				seedGateway(ctx, org.ID)
				seedBuiltinRegistry(ctx, org.ID)
			}
		}()
	} else if c != nil && c.BuiltinRegistryEndpoint != "" {
		// No gateway seeding, but still seed builtin registry on registration + startup.
		auth.onFirstRegistration = func(ctx context.Context, orgID uuid.UUID) {
			seedBuiltinRegistry(ctx, orgID)
		}
		go func() {
			time.Sleep(2 * time.Second)
			ctx := context.Background()
			var orgs []meshdb.Organization
			if err := db.WithContext(ctx).Find(&orgs).Error; err != nil || len(orgs) == 0 {
				return
			}
			for _, org := range orgs {
				seedBuiltinRegistry(ctx, org.ID)
			}
		}()
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
		Registries:      registries,
		Headscale:       headscaleSvc,
		K8s:             k8sClient,
	}
}
