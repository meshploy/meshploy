package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/client-go/kubernetes"
)

// GetNodeMetrics scrapes live resource metrics from node_exporter on the node.
// Returns a non-nil error when node_exporter is unreachable (not installed).
func (s *NodeService) GetNodeMetrics(ctx context.Context, nodeID uuid.UUID) (*NodeMetrics, error) {
	node, err := s.Get(ctx, nodeID)
	if err != nil {
		return nil, err
	}
	if node.TailscaleIP == "" {
		return nil, fmt.Errorf("node has no mesh IP")
	}
	// When the API runs in Docker it cannot reach the gateway's own Tailscale IP
	// (a host-local interface) directly. Use the Docker bridge gateway IP instead,
	// which node_exporter also listens on for gateway nodes.
	scrapeIP := node.TailscaleIP
	if s.hostGatewayIP != "" && s.gatewayIP != "" && node.TailscaleIP == s.gatewayIP {
		scrapeIP = s.hostGatewayIP
	}
	return scrapeNodeExporter(ctx, scrapeIP)
}

type NodeService struct {
	db            *gorm.DB
	gatewayIP     string // gateway mesh IP (MESH_IP) — used to detect self-scrape
	hostGatewayIP string // Docker bridge host IP (HOST_GATEWAY_IP) — used instead of gatewayIP when API is in Docker
	headscale     *HeadscaleService
	domains       *DomainService // where the public Headscale URL a joining machine needs comes from
	headscaleUser string         // HEADSCALE_USER — whose pre-auth keys those are
	notif         *NotificationService
	k8s           kubernetes.Interface // nil without a cluster; removal then skips that step
}

// StartNodeMonitor polls Headscale every 2 minutes and dispatches node.offline
// notifications when a node transitions from online to offline.
func (s *NodeService) StartNodeMonitor(ctx context.Context) {
	if s.headscale == nil || s.notif == nil {
		return
	}
	offlineNotified := make(map[uuid.UUID]bool)
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkNodeOnlineStatus(ctx, offlineNotified)
		}
	}
}

func (s *NodeService) checkNodeOnlineStatus(ctx context.Context, offlineNotified map[uuid.UUID]bool) {
	// Select on the mesh IP rather than headscale_id: a node that registered
	// while the Headscale credential was dead never got an ID stored, and
	// filtering on it would exclude that node from monitoring forever.
	var nodes []db.Node
	if err := s.db.WithContext(ctx).Where("tailscale_ip <> ''").Find(&nodes).Error; err != nil {
		return
	}
	hsNodes, err := s.headscale.ListNodes(ctx)
	if err != nil {
		return
	}
	peerByID := make(map[string]HeadscaleNode, len(hsNodes))
	peerByIP := make(map[string]HeadscaleNode, len(hsNodes))
	for _, hn := range hsNodes {
		peerByID[hn.ID] = hn
		if len(hn.IPAddresses) > 0 {
			peerByIP[hn.IPAddresses[0]] = hn
		}
	}
	for _, node := range nodes {
		peer, known := peerByID[node.HeadscaleID]
		if !known {
			// Fall back to the mesh IP and repair the missing link, so one
			// outage does not orphan a node past the end of that outage.
			if p, found := peerByIP[node.TailscaleIP]; found {
				peer, known = p, true
				if err := s.SetHeadscaleID(ctx, node.ID, p.ID); err != nil {
					log.Printf("warning: monitor backfill headscale_id for node %s: %v", node.ID, err)
				}
			}
		}
		if !known && node.HeadscaleID == "" {
			// Never linked to a mesh peer at all -- a row registered out of band
			// that never joined. There is no online -> offline transition here,
			// so stay quiet rather than inventing one.
			continue
		}
		isOnline := known && peer.Online
		wasOffline := offlineNotified[node.ID]
		if !isOnline && !wasOffline {
			offlineNotified[node.ID] = true
			s.notif.Dispatch(ctx, node.OrganizationID, "node.offline", NotificationData{
				NodeName: node.Name,
				Link:     "/nodes/" + node.ID.String(),
			})
		} else if isOnline && wasOffline {
			delete(offlineNotified, node.ID)
			// Only after an offline was reported: a node that was never
			// announced as down has nothing to come back from.
			s.notif.Dispatch(ctx, node.OrganizationID, "node.online", NotificationData{
				NodeName: node.Name,
				Link:     "/nodes/" + node.ID.String(),
			})
		}
	}
}

func (s *NodeService) List(ctx context.Context, orgID uuid.UUID) ([]db.Node, error) {
	nodes := make([]db.Node, 0)
	err := s.db.WithContext(ctx).Where("organization_id = ?", orgID).Find(&nodes).Error
	return nodes, err
}

func (s *NodeService) Get(ctx context.Context, nodeID uuid.UUID) (*db.Node, error) {
	var node db.Node
	err := s.db.WithContext(ctx).First(&node, "id = ?", nodeID).Error
	return &node, err
}

// Register creates a new node. Pass db.K3sRoleServer as the optional role to
// mark the gateway node; defaults to db.K3sRoleAgent if omitted.
func (s *NodeService) Register(ctx context.Context, orgID uuid.UUID, name, tailscaleIP string, role ...db.K3sRole) (*db.Node, error) {
	k3sRole := db.K3sRoleAgent
	if len(role) > 0 && role[0] != "" {
		k3sRole = role[0]
	}
	// Meter worker nodes only — the gateway is fixed overhead, not capacity,
	// and counting it would make an HA gateway consume the customer's allowance.
	if k3sRole != db.K3sRoleServer {
		if err := checkQuota(ctx, orgID, QuotaNode); err != nil {
			return nil, err
		}
	}
	// Server nodes are the running gateway — seed them as online.
	// Agent nodes start offline until their first heartbeat.
	status := db.NodeOffline
	if k3sRole == db.K3sRoleServer {
		status = db.NodeOnline
	}
	node := db.Node{
		OrganizationID: orgID,
		Name:           name,
		TailscaleIP:    tailscaleIP,
		Status:         status,
		K3sRole:        k3sRole,
	}
	// A machine joining from elsewhere was handed the primary's headscale name,
	// and keeps using it until it is moved. The gateway reaches Headscale over
	// loopback and depends on no name at all.
	if k3sRole != db.K3sRoleServer && s.domains != nil {
		node.ControlURL = s.domains.PlatformURL(ctx, orgID, "headscale")
	}
	err := s.db.WithContext(ctx).Create(&node).Error
	return &node, err
}

type UpdateNodeInput struct {
	Name     string      // empty = no change
	K3sRole  db.K3sRole  // empty = no change
	MeshRole db.MeshRole // empty = no change
}

// ErrMeshRoleSwitch refuses moving a node into or out of the mesh-only role:
// that means installing or removing K3s on the machine, which the API cannot do.
var ErrMeshRoleSwitch = errors.New("a node moves into or out of the cluster on the machine itself: " +
	"a mesh-only node joins by installing K3s there, and a cluster node becomes mesh only by removing it")

func (s *NodeService) Update(ctx context.Context, nodeID uuid.UUID, in UpdateNodeInput) (*db.Node, error) {
	var node db.Node
	if err := s.db.WithContext(ctx).First(&node, "id = ?", nodeID).Error; err != nil {
		return nil, err
	}
	if in.MeshRole != "" && in.MeshRole != node.MeshRole &&
		(in.MeshRole == db.MeshRoleMesh || node.MeshRole == db.MeshRoleMesh) {
		return nil, ErrMeshRoleSwitch
	}
	updates := map[string]any{}
	if in.Name != "" {
		updates["name"] = in.Name
	}
	if in.K3sRole != "" {
		updates["k3s_role"] = in.K3sRole
	}
	if in.MeshRole != "" {
		updates["mesh_role"] = in.MeshRole
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&node).Updates(updates).Error; err != nil {
			return nil, err
		}
		// Re-fetch so the returned struct reflects the persisted values.
		if err := s.db.WithContext(ctx).First(&node, "id = ?", nodeID).Error; err != nil {
			return nil, err
		}
	}
	return &node, nil
}

// SetHeadscaleID stores the Headscale peer ID on the node for stable lookups.
// Called after self-registration and as a lazy backfill during enrichNodes.
func (s *NodeService) SetPublicIP(ctx context.Context, nodeID uuid.UUID, publicIP string) error {
	return s.db.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Update("public_ip", publicIP).Error
}

func (s *NodeService) SetHeadscaleID(ctx context.Context, nodeID uuid.UUID, headscaleID string) error {
	return s.db.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Update("headscale_id", headscaleID).Error
}

// UpdateRole sets the k3s role on a node. Used internally during gateway seeding.
func (s *NodeService) UpdateRole(ctx context.Context, nodeID uuid.UUID, role db.K3sRole) error {
	return s.db.WithContext(ctx).Model(&db.Node{}).Where("id = ?", nodeID).Update("k3s_role", role).Error
}

func (s *NodeService) Delete(ctx context.Context, nodeID uuid.UUID) error {
	return s.db.WithContext(ctx).Delete(&db.Node{}, "id = ?", nodeID).Error
}

// GenerateRegistrationToken creates or replaces the org's node registration
// token and returns the new token string. Format: mreg-<32 random hex bytes>.
func (s *NodeService) GenerateRegistrationToken(ctx context.Context, orgID uuid.UUID) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := "mreg-" + hex.EncodeToString(raw)

	row := db.NodeRegistrationToken{
		OrganizationID: orgID,
		Token:          token,
	}
	// Upsert: replace token if one already exists for this org.
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "organization_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"token", "updated_at"}),
		}).
		Create(&row).Error
	if err != nil {
		return "", err
	}
	return token, nil
}

// OrgIDFromToken resolves a registration token to its organisation ID.
// Returns an error if the token is invalid.
func (s *NodeService) OrgIDFromToken(ctx context.Context, token string) (uuid.UUID, error) {
	var row db.NodeRegistrationToken
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return uuid.Nil, fmt.Errorf("invalid registration token")
		}
		return uuid.Nil, err
	}
	return row.OrganizationID, nil
}

// GetRegistrationToken returns the current registration token for the org,
// or an empty string if none has been generated yet.
func (s *NodeService) GetRegistrationToken(ctx context.Context, orgID uuid.UUID) (string, error) {
	var row db.NodeRegistrationToken
	err := s.db.WithContext(ctx).
		Where("organization_id = ?", orgID).
		First(&row).Error
	if err == gorm.ErrRecordNotFound {
		return "", nil
	}
	return row.Token, err
}

// RegisterWithToken validates a node registration token and creates the node.
// Returns the new node, or an error if the token is invalid.
// An optional MeshRole sets the scheduling role (defaults to MeshRoleWorkloadBuilder).
func (s *NodeService) RegisterWithToken(ctx context.Context, token, name, tailscaleIP string, meshRole ...db.MeshRole) (*db.Node, error) {
	var row db.NodeRegistrationToken
	if err := s.db.WithContext(ctx).Where("token = ?", token).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("invalid registration token")
		}
		return nil, err
	}
	node, err := s.Register(ctx, row.OrganizationID, name, tailscaleIP)
	if err != nil {
		return nil, err
	}
	role := db.MeshRoleWorkloadBuilder
	if len(meshRole) > 0 && meshRole[0] != "" {
		role = meshRole[0]
	}
	if err := s.db.WithContext(ctx).Model(node).Update("mesh_role", role).Error; err != nil {
		return nil, err
	}
	node.MeshRole = role
	return node, nil
}

// ─── Provisioning tokens ──────────────────────────────────────────────────────

// CreateProvisioningToken generates a single-use provisioning token for the org.
// Format: mprov-<32 random hex bytes>. The plaintext is returned once — only
// its SHA-256 hash is persisted.
func (s *NodeService) CreateProvisioningToken(ctx context.Context, orgID uuid.UUID, label string, expiresAt *time.Time, meshRole ...db.MeshRole) (string, *db.NodeProvisioningToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate provisioning token: %w", err)
	}
	plaintext := "mprov-" + hex.EncodeToString(raw)

	// A token with no expiry given gets one anyway.
	//
	// This is a credential that joins a machine to the mesh, and it is made to
	// be pasted into a terminal within the minute. Before this, one generated
	// and then abandoned - a wrong role picked, a tab closed - stayed valid for
	// ever, unlisted and unrevokable. A caller who genuinely needs longer says
	// so; nobody who forgot has to.
	if expiresAt == nil {
		at := time.Now().Add(provisioningTokenTTL)
		expiresAt = &at
	}
	row := db.NodeProvisioningToken{
		OrganizationID: orgID,
		TokenHash:      hashToken(plaintext),
		Label:          label,
		ExpiresAt:      expiresAt,
	}
	if len(meshRole) > 0 {
		row.MeshRole = meshRole[0]
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return "", nil, err
	}
	return plaintext, &row, nil
}

// Provisioning is what a machine is told before it can join anything.
//
// It is deliberately the smaller half of joining. The call is public - a
// machine being provisioned has no session and no mesh address yet - so it
// hands back only what is needed to reach the mesh: where Headscale is, and a
// key to join it with. Everything else, the k3s join token included, is asked
// for afterwards from inside the mesh, where the caller has had to prove it got
// there. A leaked one-liner then costs an unknown peer on the mesh, which
// Headscale can be told to drop, and not the cluster's join token.
type Provisioning struct {
	HeadscaleURL string      `json:"headscale_url"`
	PreAuthKey   string      `json:"preauth_key"`
	APIMeshURL   string      `json:"api_mesh_url"`
	MeshRole     db.MeshRole `json:"mesh_role"`
}

// Provision validates a provisioning token and mints the mesh credentials for
// one machine.
//
// It does not consume the token: registration does that, from inside the mesh,
// once the machine has actually joined. What it does stamp is ProvisionedAt, so
// one token cannot mint mesh credentials over and over.
func (s *NodeService) Provision(ctx context.Context, token string) (*Provisioning, error) {
	// The credential is checked before anything about this gateway is: a
	// caller holding a token that is not good has no business learning how the
	// server is configured, and a token that is spent is spent whatever the
	// mesh looks like.
	var row db.NodeProvisioningToken
	if err := s.db.WithContext(ctx).Where("token_hash = ?", hashToken(token)).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("invalid provisioning token")
		}
		return nil, err
	}
	if row.UsedAt != nil {
		return nil, fmt.Errorf("provisioning token already used")
	}
	if row.ProvisionedAt != nil {
		return nil, fmt.Errorf("provisioning token already provisioned a machine")
	}
	if row.ExpiresAt != nil && time.Now().After(*row.ExpiresAt) {
		return nil, fmt.Errorf("provisioning token expired")
	}

	if s.headscale == nil {
		return nil, fmt.Errorf("this gateway has no mesh: Headscale is not configured")
	}
	// The machine asking is outside the gateway, so it needs the name the
	// internet reaches Headscale by - not the address the API uses for it.
	controlURL := ""
	if s.domains != nil {
		controlURL = s.domains.PlatformURL(ctx, row.OrganizationID, "headscale")
	}
	if controlURL == "" {
		return nil, fmt.Errorf("this gateway has no public domain for a machine to join through")
	}

	user := s.headscaleUser
	if user == "" {
		user = "meshploy"
	}
	key, err := s.headscale.CreatePreAuthKeyWith(ctx, user, false, preAuthKeyTTL)
	if err != nil {
		return nil, fmt.Errorf("mint a mesh key: %w", err)
	}

	now := time.Now()
	if err := s.db.WithContext(ctx).Model(&row).Update("provisioned_at", &now).Error; err != nil {
		return nil, err
	}

	role := row.MeshRole
	if role == "" {
		role = db.MeshRoleWorkloadBuilder
	}
	gateway := s.gatewayIP
	if gateway == "" {
		gateway = "100.64.0.1"
	}
	return &Provisioning{
		HeadscaleURL: controlURL,
		PreAuthKey:   key.Key,
		APIMeshURL:   fmt.Sprintf("http://%s:4000", gateway),
		MeshRole:     role,
	}, nil
}

// preAuthKeyTTL is how long a provisioned machine has to join. Minutes, not
// months: the machine redeems it seconds after being handed it, and the key
// travels in a command line that will outlive its usefulness in somebody's
// shell history.
const preAuthKeyTTL = time.Hour

// provisioningTokenTTL is how long an unexpiring request gets instead.
const provisioningTokenTTL = time.Hour

// RegisterWithProvisioningToken validates a single-use provisioning token and
// creates the node. On success it:
//   - stamps UsedAt on the token (preventing re-use)
//   - generates a per-node secret (mnode-<hex>) and stores its hash on the node
//   - returns the new node and the plaintext node secret (shown once)
func (s *NodeService) RegisterWithProvisioningToken(ctx context.Context, token, name, tailscaleIP string, meshRole db.MeshRole) (*db.Node, string, error) {
	hash := hashToken(token)

	var row db.NodeProvisioningToken
	if err := s.db.WithContext(ctx).Where("token_hash = ?", hash).First(&row).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, "", fmt.Errorf("invalid provisioning token")
		}
		return nil, "", err
	}
	if row.UsedAt != nil {
		return nil, "", fmt.Errorf("provisioning token already used")
	}
	if row.ExpiresAt != nil && time.Now().After(*row.ExpiresAt) {
		return nil, "", fmt.Errorf("provisioning token expired")
	}

	node, err := s.Register(ctx, row.OrganizationID, name, tailscaleIP)
	if err != nil {
		return nil, "", err
	}

	role := db.MeshRoleWorkloadBuilder
	if meshRole != "" {
		role = meshRole
	}

	// Generate per-node secret
	secretRaw := make([]byte, 32)
	if _, err := rand.Read(secretRaw); err != nil {
		return nil, "", fmt.Errorf("generate node secret: %w", err)
	}
	nodeSecret := "mnode-" + hex.EncodeToString(secretRaw)

	// Persist role + secret hash on node, stamp token as used
	now := time.Now()
	if err := s.db.WithContext(ctx).Model(node).Updates(map[string]any{
		"mesh_role":        role,
		"node_secret_hash": hashToken(nodeSecret),
	}).Error; err != nil {
		return nil, "", err
	}
	if err := s.db.WithContext(ctx).Model(&row).Update("used_at", &now).Error; err != nil {
		return nil, "", err
	}

	node.MeshRole = role
	return node, nodeSecret, nil
}

// ValidateNodeSecret checks that the given secret matches the hash stored on
// the node. Returns nil on success, an error if the secret is wrong or the node
// has no secret (registered via legacy mreg token).
func (s *NodeService) ValidateNodeSecret(ctx context.Context, nodeID uuid.UUID, secret string) error {
	hash := hashToken(secret)
	var node db.Node
	err := s.db.WithContext(ctx).
		Where("id = ? AND node_secret_hash = ?", nodeID, hash).
		First(&node).Error
	if err == gorm.ErrRecordNotFound {
		return fmt.Errorf("invalid node secret")
	}
	return err
}
