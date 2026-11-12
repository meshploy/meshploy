package service

import (
	"context"
	"github.com/meshploy/packages/hostagent"
	"log"
	"os"
	"time"

	"github.com/google/uuid"
	meshdb "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/config"
	appk8s "github.com/meshploy/packages/server/k8s"
	"github.com/meshploy/packages/server/templates"
	"gorm.io/gorm"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

type Services struct {
	Auth            *AuthService
	Entitlements    *EntitlementService
	Agents          *AgentService
	CLILogins       *CLILoginService
	SignInLinks     *SignInLinkService
	Sessions        *ConsoleSessionService
	OAuth           *OAuthService
	MeshAccess      *MeshAccessService
	Orgs            *OrgService
	Permissions     *PermissionService
	Projects        *ProjectService
	Promotions      *PromotionService
	Nodes           *NodeService
	Workloads       *WorkloadService
	ConfigFiles     *ConfigFileService
	Orphans         *OrphanService
	Placement       *PlacementService
	ProjectMaps     *ProjectMapService
	Stacks          *StackService
	Volumes         *VolumeService
	Domains         *DomainService
	Brands          *BrandService
	Routes          *RouteService
	TCPRoutes       *TCPRouteService
	EdgeFallback    *EdgeFallbackService
	Edge            *EdgeService
	Deployments     *DeploymentService
	Activity        *ActivityService
	Overview        *OverviewService
	GitIntegrations *GitIntegrationService
	Registries      *RegistryService
	Storage         *StorageService
	DNSProviders    *DNSProviderService
	Backups         *BackupService
	System          *SystemService
	Notifications   *NotificationService
	EmailConfig     *EmailConfigService
	VariableGroups  *VariableGroupService
	Jobs            *JobService
	Templates       *TemplateService
	DBExplorer      *DBExplorerService
	Tickets         *TicketService    // single-use tickets for WebSocket auth
	Headscale       *HeadscaleService // nil if HEADSCALE_URL / HEADSCALE_API_KEY not set
	Isolation       *IsolationService
	Sandbox         *SandboxService
	K8s             kubernetes.Interface // nil if KUBECONFIG unavailable
	// Cluster is how the cluster is reached: its connection for exec and
	// streams, and the host-level pods (builder, image cleanup, node
	// terminal). Nil exactly when K8s is.
	Cluster appk8s.Driver
	// Agent is the cluster when this API reaches it through a cluster agent
	// (config ClusterAgent), which registration points at the agent; nil
	// otherwise.
	Agent *appk8s.AgentCluster
	// DB is the database, for an extension's own tables. Core code goes
	// through the services above.
	DB *gorm.DB
}

func New(db *gorm.DB, cfg ...*config.Config) *Services {
	var c *config.Config
	if len(cfg) > 0 {
		c = cfg[0]
	}

	// K8s client is optional - log a warning if not available.
	var k8sClient kubernetes.Interface
	var k8sRestCfg *rest.Config
	var cluster appk8s.Driver
	var agent *appk8s.AgentCluster
	if c != nil && c.ClusterAgent {
		// The cluster is reached only through its cluster agent, once one is
		// registered; never through a kubeconfig, which this API does not hold.
		a, err := appk8s.NewAgentCluster()
		if err != nil {
			log.Printf("warning: cluster agent client: %v; build/deploy features disabled", err)
		} else {
			agent, cluster, k8sClient = a, a, a.Client()
		}
	} else if c != nil {
		var err error
		k8sClient, k8sRestCfg, err = appk8s.NewClientWithOptions(appk8s.Options{
			KubeconfigPath: c.KubeconfigPath,
			ServerURL:      c.K3sServerURL,
			TLSServerName:  c.K3sTLSServerName,
			SkipTLSVerify:  c.K3sSkipTLSVerify,
		})
		if err != nil {
			log.Printf("warning: K8s not available (%v) — build/deploy features disabled", err)
			// NewClientWithConfig returns a typed-nil *Clientset on error, which
			// becomes a NON-nil kubernetes.Interface once assigned above. Blank it
			// explicitly so downstream `if k8s != nil` guards actually skip K8s work
			// instead of dereferencing a nil clientset and panicking.
			k8sClient, k8sRestCfg = nil, nil
		} else {
			cluster = appk8s.NewLocalDriver(k8sClient, k8sRestCfg)
			go appk8s.CleanupOrphanedShellPods(context.Background(), cluster)
		}
	}

	// Built first: git integrations and notifications both need the primary
	// domain's platform addresses.
	dnsProviders := &DNSProviderService{db: db}
	domains := &DomainService{db: db, dns: dnsProviders}
	if c != nil {
		domains.installDomain = c.Domain
		domains.publicIP = c.PublicIP
		domains.fixed = c.PlatformURLs
		// Before anything reads a mode: a domain row from before the column
		// would otherwise be served as a delegation it never was.
		if err := domains.BackfillDNSModes(context.Background(), c.DNSMode); err != nil {
			log.Printf("warning: backfill domain DNS modes: %v", err)
		}
	}
	gitSvc := &GitIntegrationService{db: db, cfg: c, domains: domains}
	domains.git = gitSvc

	var headscaleSvc *HeadscaleService
	if c != nil && c.HeadscaleURL != "" && c.HeadscaleKey != "" {
		headscaleSvc = NewHeadscaleService(c.HeadscaleURL, c.HeadscaleKey)
	}

	var gatewayIP, hostGatewayIP string
	if c != nil {
		gatewayIP = c.GatewayIP
		hostGatewayIP = c.HostGatewayIP
	}
	notif := &NotificationService{db: db, domains: domains}
	if c != nil {
		notif.consoleURL = c.FrontendURL
	}
	nodes := &NodeService{db: db, gatewayIP: gatewayIP, hostGatewayIP: hostGatewayIP}
	if c != nil {
		nodes.apiPort = c.APIPort
	}
	sessions := &ConsoleSessionService{db: db}
	auth := &AuthService{db: db, sessions: sessions}
	if c != nil {
		auth.setupToken = c.SetupToken
	}
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
		// Through a cluster agent the gateway is the agent's machine, which
		// registers itself; MESH_IP is the API's own machine, no node.
		if c.ClusterAgent {
			if err := domains.CreateSeeded(ctx, orgID, c.Domain, meshdb.DNSMode(c.DNSMode)); err != nil {
				log.Printf("warning: seed domain: %v", err)
			}
			return
		}
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
		// The gateway builds by default, so a single-server install can deploy
		// from git with nothing to configure. Only an unset role is filled in:
		// turning "Act as build node" off stores workload, which stays as it is.
		if err := db.WithContext(ctx).Model(&meshdb.Node{}).
			Where("organization_id = ? AND tailscale_ip = ? AND mesh_role = ?", orgID, c.GatewayIP, "").
			Update("mesh_role", meshdb.MeshRoleWorkloadBuilder).Error; err != nil {
			log.Printf("warning: default the gateway to a build node: %v", err)
		}
		if err := domains.CreateSeeded(ctx, orgID, c.Domain, meshdb.DNSMode(c.DNSMode)); err != nil {
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

	varGroups := &VariableGroupService{db: db}
	if c != nil {
		// Published groups carry each service's address; correct any an
		// earlier version wrote wrong, in the background.
		go func() {
			if err := varGroups.RefreshPublished(context.Background()); err != nil {
				log.Printf("warning: refresh published variable groups: %v", err)
			}
		}()
	}
	workloads := &WorkloadService{db: db, k8s: k8sClient, varGroups: varGroups}
	// On a gateway the install recorded where published ports are bound; an
	// agent API learns it from the edge's facts (NodeService.SyncEdgeFacts).
	workloads.nodePortMeshOnly.Store(c != nil && c.NodePortAddresses != "")
	projects := &ProjectService{db: db}
	sandbox := &SandboxService{db: db, k8s: k8sClient}
	deployments := &DeploymentService{db: db, cfg: c, k8s: k8sClient, cluster: cluster, git: gitSvc, varGroups: varGroups, notif: notif, sandbox: sandbox}

	backups := &BackupService{db: db, k8s: k8sClient, cluster: cluster, cfg: c, sem: make(chan struct{}, maxConcurrentBackups), notif: notif}

	volumes := &VolumeService{db: db, k8s: k8sClient, deployment: deployments}
	routes := &RouteService{db: db, k8s: k8sClient}
	deployments.routes = routes
	// The host agent's folder: on the cluster agent's machine when the
	// cluster is reached through one, which is where the edge and its
	// registry are; otherwise on this one.
	var host hostagent.Folder
	if agent != nil {
		host = agent.HostFolder()
	}
	host = hostFolder(host, c)
	deployments.host = host
	nodes.host = host
	nodes.nodePorts = workloads.nodePortMeshOnly.Store
	tcpRoutes := &TCPRouteService{db: db, k8s: k8sClient, host: host}
	workloads.tcpRoutes = tcpRoutes
	deployments.tcpRoutes = tcpRoutes
	workloads.notif = notif
	workloads.git = gitSvc
	configFiles := &ConfigFileService{db: db, deployment: deployments}
	deployments.configFiles = configFiles

	nodes.headscale = headscaleSvc
	nodes.domains = domains
	if c != nil {
		nodes.headscaleUser = c.HeadscaleUser
		nodes.k3sToken = c.K3sToken
	}
	nodes.notif = notif
	nodes.k8s = k8sClient
	nodes.cluster = cluster
	// Workloads need the deployment service to re-apply a Deployment from current
	// config on start; the two are wired after construction because deployments
	// is built from workloads' dependencies.
	workloads.deployment = deployments

	entitlements := &EntitlementService{db: db, domain: entitlementDomain(c)}
	agents := &AgentService{db: db, notif: notif}

	isolation := newIsolationService(db, k8sClient)
	svc := &Services{
		Isolation:       isolation,
		Sandbox:         sandbox,
		DB:              db,
		Auth:            auth,
		Entitlements:    entitlements,
		Agents:          agents,
		CLILogins:       &CLILoginService{db: db},
		SignInLinks:     &SignInLinkService{db: db},
		Sessions:        sessions,
		OAuth:           &OAuthService{db: db},
		MeshAccess:      &MeshAccessService{db: db, headscale: headscaleSvc, api: meshAPIOf(c)},
		Orgs:            &OrgService{db: db, notif: notif},
		Permissions:     &PermissionService{db: db},
		Projects:        projects,
		Promotions:      &PromotionService{db: db, projects: projects, deployments: deployments, workloads: workloads, backups: backups},
		ConfigFiles:     configFiles,
		Orphans:         &OrphanService{db: db, k8s: k8sClient, workloads: workloads},
		Placement:       &PlacementService{db: db, k8s: k8sClient, workloads: workloads},
		Stacks:          &StackService{db: db, git: gitSvc, workload: workloads, volumes: volumes, routes: routes, configFiles: configFiles, deployment: deployments},
		Nodes:           nodes,
		Workloads:       workloads,
		Volumes:         volumes,
		Domains:         domains,
		Brands:          &BrandService{db: db},
		Routes:          routes,
		TCPRoutes:       tcpRoutes,
		EdgeFallback:    &EdgeFallbackService{db: db},
		Edge:            NewEdgeService(db, c != nil && c.ClusterAgent),
		Deployments:     deployments,
		Activity:        &ActivityService{db: db},
		GitIntegrations: gitSvc,
		Registries:      registries,
		Storage:         &StorageService{db: db},
		DNSProviders:    dnsProviders,
		Backups:         backups,
		System:          &SystemService{db: db, cfg: c, ent: entitlements, agents: agents, hostFolder: host},
		Notifications:   notif,
		EmailConfig:     &EmailConfigService{db: db},
		VariableGroups:  varGroups,
		Jobs: func() *JobService {
			j := &JobService{db: db, k8s: k8sClient, notif: notif, varGroups: varGroups, sandbox: sandbox}
			if k8sClient != nil {
				go j.StartReconciler(context.Background())
			}
			return j
		}(),
		DBExplorer: &DBExplorerService{db: db, k8s: k8sClient, cluster: cluster},
		Headscale:  headscaleSvc,
		Tickets:    NewTicketService(),
		K8s:        k8sClient,
		Cluster:    cluster,
		Agent:      agent,
	}

	// Template service - depends on the stack + route services, so wire it after
	// the aggregate is built. The catalog is remote (GitHub) by default, or a
	// local dir when TEMPLATE_DIR is set.
	svc.Templates = &TemplateService{
		db:      db,
		cfg:     c,
		catalog: newTemplateCatalog(c),
		stacks:  svc.Stacks,
		routes:  svc.Routes,
	}

	// Keep stored service status in step with the cluster.
	if k8sClient != nil {
		go workloads.StartStatusReconciler(context.Background())
		go volumes.StartVolumeStatusReconciler(context.Background())
	}

	go backups.StartScheduler(context.Background())
	go backups.StartRetentionReaper(context.Background())
	go deployments.StartRegistryGC(context.Background())
	// What an API restart left in flight - a build being waited on, a rollout
	// being watched, a stack rolling out layer by layer - is picked up again
	// once the process has settled, instead of left "pending" for good.
	if c != nil && deployments.K8sConfigured() {
		go func() {
			time.Sleep(resumeAfter)
			ctx := context.Background()
			deployments.ResumeInFlight(ctx)
			svc.Stacks.ResumeRuns(ctx)
		}()
	}
	go func() {
		time.Sleep(resumeAfter)
		svc.System.SyncEdge(context.Background())
	}()
	go notif.StartDeliveryReaper(context.Background())
	go gitSvc.StartAbandonedAppReaper(context.Background())
	go nodes.StartNodeMonitor(context.Background())
	go nodes.StartRemovalWorker(context.Background())
	go svc.MeshAccess.Follow(context.Background())
	go svc.Isolation.Start(context.Background())
	if agent != nil {
		// What a local cluster gets at startup, an agent's cluster gets each
		// time its agent is set: at startup if one is registered, and at
		// registration otherwise.
		nodes.agent = agent
		nodes.onClusterAgent = append(nodes.onClusterAgent, func(ctx context.Context) {
			appk8s.CleanupOrphanedShellPods(ctx, agent)
			if err := appk8s.EnsureSandboxRuntimeClass(ctx, agent.Client()); err != nil {
				log.Printf("warning: sandbox runtime class: %v", err)
			}
			// The edge is on the agent's machine and reached only through it,
			// so it is told what to serve as soon as it can be, at the address
			// it reports.
			if _, err := nodes.SyncEdgeFacts(ctx); err != nil {
				log.Printf("warning: the edge's facts: %v", err)
			}
			// Its registry is the built-in one: builds push there.
			if err := registries.SyncEdgeBuiltin(ctx); err != nil {
				log.Printf("warning: the edge's registry: %v", err)
			}
			if err := svc.System.PublishPrimaryEdge(ctx); err != nil {
				log.Printf("warning: could not hand the edge its domain set: %v", err)
			}
		})
		go nodes.FollowEdgeFacts(context.Background(), svc.System.PublishPrimaryEdge)
		go func() {
			if err := nodes.LoadClusterAgent(context.Background()); err != nil {
				log.Printf("warning: cluster agent: %v", err)
			}
		}()
	} else if k8sClient != nil {
		go func() {
			if err := appk8s.EnsureSandboxRuntimeClass(context.Background(), k8sClient); err != nil {
				log.Printf("warning: sandbox runtime class: %v", err)
			}
		}()
	}

	svc.ProjectMaps = &ProjectMapService{db: db, workloads: workloads, stacks: svc.Stacks, routes: routes, volumes: volumes}
	svc.Overview = &OverviewService{db: db, projects: projects, promotions: svc.Promotions, orphans: svc.Orphans, workloads: workloads, metrics: nodes.GetNodeMetrics}
	return svc
}

// newTemplateCatalog builds the one-click template catalog. A local TEMPLATE_DIR
// (dev / offline / air-gapped) takes precedence; otherwise the catalog is fetched
// from the public GitHub repo (TEMPLATE_REPO) and refreshed in the background.
func newTemplateCatalog(c *config.Config) templates.Catalog {
	if c == nil {
		return templates.NewEmbeddedCatalog() // pinned snapshot — never empty
	}
	if c.TemplateDir != "" {
		return templates.NewRegistry(os.DirFS(c.TemplateDir), ".")
	}
	remote := templates.NewRemoteCatalog(
		c.TemplateRepo, c.TemplateRepoRef, c.TemplateRefreshInterval,
		templates.NewEmbeddedCatalog(), // fallback while the live catalog is empty
	)
	go remote.StartRefresh(context.Background())
	return remote
}

// meshAPIOf is the API's own machine on the mesh, for an API that reaches its
// cluster through a cluster agent: there the gateway is the agent's machine,
// and the API's is another.
func meshAPIOf(c *config.Config) *MeshAPI {
	if c == nil || !c.ClusterAgent || c.GatewayIP == "" {
		return nil
	}
	port := c.APIPort
	if port == 0 {
		port = 4000
	}
	return &MeshAPI{IP: c.GatewayIP, Port: port}
}
