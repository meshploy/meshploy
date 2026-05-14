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
	"k8s.io/client-go/rest"
)

type Services struct {
	Auth            *AuthService
	Orgs            *OrgService
	Projects        *ProjectService
	Nodes           *NodeService
	Workloads       *WorkloadService
	Stacks          *StackService
	Volumes         *VolumeService
	Domains         *DomainService
	Routes          *RouteService
	Deployments     *DeploymentService
	GitIntegrations *GitIntegrationService
	Registries      *RegistryService
	Storage         *StorageService
	Backups         *BackupService
	Notifications   *NotificationService
	EmailConfig     *EmailConfigService
	Secrets         *SecretService
	Jobs            *JobService
	DBExplorer      *DBExplorerService
	Headscale       *HeadscaleService    // nil if HEADSCALE_URL / HEADSCALE_API_KEY not set
	K8s             kubernetes.Interface // nil if KUBECONFIG unavailable
	K8sRestConfig   *rest.Config         // nil if KUBECONFIG unavailable
}

func New(db *gorm.DB, cfg ...*config.Config) *Services {
	var c *config.Config
	if len(cfg) > 0 {
		c = cfg[0]
	}

	// K8s client is optional — log a warning if not available.
	var k8sClient kubernetes.Interface
	var k8sRestCfg *rest.Config
	if c != nil {
		var err error
		k8sClient, k8sRestCfg, err = appk8s.NewClientWithConfig(c.KubeconfigPath, c.K3sServerURL)
		if err != nil {
			log.Printf("warning: K8s not available (%v) — build/deploy features disabled", err)
		} else {
			go appk8s.CleanupOrphanedShellPods(context.Background(), k8sClient)
		}
	}

	gitSvc := &GitIntegrationService{db: db, cfg: c}

	var headscaleSvc *HeadscaleService
	if c != nil && c.HeadscaleURL != "" && c.HeadscaleKey != "" {
		headscaleSvc = NewHeadscaleService(c.HeadscaleURL, c.HeadscaleKey)
	}

	var gatewayIP, hostGatewayIP string
	if c != nil {
		gatewayIP = c.GatewayIP
		hostGatewayIP = c.HostGatewayIP
	}
	nodes := &NodeService{db: db, gatewayIP: gatewayIP, hostGatewayIP: hostGatewayIP}
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
			node, err := nodes.Register(ctx, orgID, c.GatewayHostname, c.GatewayIP, meshdb.K3sRoleServer)
			if err != nil {
				log.Printf("warning: seed gateway node: %v", err)
			} else if c.PublicIP != "" {
				if err := nodes.SetPublicIP(ctx, node.ID, c.PublicIP); err != nil {
					log.Printf("warning: set gateway public IP: %v", err)
				}
			}
		} else if c.PublicIP != "" {
			// Backfill PublicIP on existing gateway node if not yet set.
			var gw meshdb.Node
			if db.WithContext(ctx).Where("organization_id = ? AND tailscale_ip = ?", orgID, c.GatewayIP).
				First(&gw).Error == nil && gw.PublicIP == "" {
				if err := nodes.SetPublicIP(ctx, gw.ID, c.PublicIP); err != nil {
					log.Printf("warning: backfill gateway public IP: %v", err)
				}
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

	workloads := &WorkloadService{db: db, k8s: k8sClient}
	deployments := &DeploymentService{db: db, cfg: c, k8s: k8sClient, git: gitSvc, secrets: &SecretService{db: db}}

	return &Services{
		Auth:            auth,
		Orgs:            &OrgService{db: db},
		Projects:        &ProjectService{db: db},
		Nodes:           nodes,
		Workloads:       workloads,
		Stacks:          &StackService{db: db, workload: workloads},
		Volumes:         &VolumeService{db: db, k8s: k8sClient, deployment: deployments},
		Domains:         domains,
		Routes:          &RouteService{db: db},
		Deployments:     deployments,
		GitIntegrations: gitSvc,
		Registries:      registries,
		Storage:         &StorageService{db: db},
		Backups:         &BackupService{db: db},
		Notifications:   &NotificationService{db: db},
		EmailConfig:     &EmailConfigService{db: db},
		Secrets:         &SecretService{db: db},
		Jobs:            func() *JobService {
			svc := &JobService{db: db, k8s: k8sClient}
			if k8sClient != nil {
				go svc.StartReconciler(context.Background())
			}
			return svc
		}(),
		DBExplorer:      &DBExplorerService{db: db, k8s: k8sClient, restCfg: k8sRestCfg},
		Headscale:       headscaleSvc,
		K8s:             k8sClient,
		K8sRestConfig:   k8sRestCfg,
	}
}
