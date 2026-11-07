package service

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	appk8s "github.com/meshploy/packages/server/k8s"
	"gorm.io/gorm"
)

// ClusterAgentTokenPrefix marks the token an API presents to a cluster agent.
const ClusterAgentTokenPrefix = "mcag-"

// ClusterAgentRegistration is what a cluster agent sends when its node
// registers: the certificate the API will pin, the token it will present, and
// the port the agent listens on at the node's mesh address.
type ClusterAgentRegistration struct {
	CertPEM string
	Token   string
	Port    int
}

var (
	// ErrClusterAgentNotExpected is an agent registering with an API that
	// reaches its cluster directly, or a token for one minted there.
	ErrClusterAgentNotExpected = errors.New("this server reaches its cluster directly and takes no cluster agent")
	// ErrClusterAgentToken is an agent registering with a token not made for
	// one, or a token made for one used without an agent.
	ErrClusterAgentToken = errors.New("a cluster agent registers only with a provisioning token made for it, and such a token registers nothing else")
	// ErrClusterAgentRegistered is a second agent: the first one's identity
	// stays until an owner or admin revokes it.
	ErrClusterAgentRegistered = errors.New("a cluster agent is already registered; revoke it first")
	// ErrClusterAgentIdentity is a certificate, token or port that cannot be
	// an agent's.
	ErrClusterAgentIdentity = errors.New("the cluster agent's certificate, token or port is not valid")
	// ErrClusterAgentAddress is a mesh address that is not the registering
	// machine's.
	ErrClusterAgentAddress = errors.New("the cluster agent's mesh address is not a machine on the mesh")
	// ErrNoClusterAgentOnNode is a revocation for a node that carries no agent.
	ErrNoClusterAgentOnNode = errors.New("this node carries no cluster agent")
)

// checkClusterAgentRegistration decides whether a registration may carry, or
// must carry, a cluster agent. Whoever writes an agent's identity receives
// every cluster request the API makes, secrets included, so only a token made
// for an agent, on an API expecting one, with no agent yet, registers one.
func (s *NodeService) checkClusterAgentRegistration(ctx context.Context, row *db.NodeProvisioningToken, reg *ClusterAgentRegistration, tailscaleIP string) error {
	if !row.ClusterAgent && reg == nil {
		return nil
	}
	if row.ClusterAgent != (reg != nil) {
		return ErrClusterAgentToken
	}
	if s.agent == nil {
		return ErrClusterAgentNotExpected
	}
	if err := validClusterAgent(reg); err != nil {
		return err
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&db.Node{}).Where("cluster_agent_cert <> ''").Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return ErrClusterAgentRegistered
	}
	return s.checkMeshAddress(ctx, tailscaleIP)
}

// checkMeshAddress refuses an address no machine on the mesh holds. The
// pinned certificate already keeps the API from talking to the wrong machine;
// this keeps a registration from naming an address that is not a peer at all.
func (s *NodeService) checkMeshAddress(ctx context.Context, ip string) error {
	if net.ParseIP(ip) == nil {
		return ErrClusterAgentAddress
	}
	if s.headscale == nil {
		return nil
	}
	peers, err := s.headscale.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("check the cluster agent's mesh address: %w", err)
	}
	for _, p := range peers {
		for _, a := range p.IPAddresses {
			if a == ip {
				return nil
			}
		}
	}
	return ErrClusterAgentAddress
}

func validClusterAgent(reg *ClusterAgentRegistration) error {
	if reg.Port < 1 || reg.Port > 65535 {
		return fmt.Errorf("%w: port %d", ErrClusterAgentIdentity, reg.Port)
	}
	hexPart, ok := strings.CutPrefix(reg.Token, ClusterAgentTokenPrefix)
	if !ok || len(hexPart) != 64 {
		return fmt.Errorf("%w: the token is not an agent's", ErrClusterAgentIdentity)
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		return fmt.Errorf("%w: the token is not an agent's", ErrClusterAgentIdentity)
	}
	block, rest := pem.Decode([]byte(reg.CertPEM))
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(rest))) != 0 {
		return fmt.Errorf("%w: the certificate is not one PEM certificate", ErrClusterAgentIdentity)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrClusterAgentIdentity, err)
	}
	if err := cert.VerifyHostname(appk8s.ClusterAgentServerName); err != nil {
		return fmt.Errorf("%w: the certificate does not name the cluster agent", ErrClusterAgentIdentity)
	}
	return nil
}

func clusterAgentIdentity(n *db.Node) appk8s.AgentIdentity {
	return appk8s.AgentIdentity{
		Addr:    net.JoinHostPort(n.TailscaleIP, strconv.Itoa(n.ClusterAgentPort)),
		CertPEM: []byte(n.ClusterAgentCert),
		Token:   string(n.ClusterAgentToken),
	}
}

// attachClusterAgent points the API's cluster at a node's agent and runs what
// a newly reached cluster needs.
func (s *NodeService) attachClusterAgent(ctx context.Context, n *db.Node) error {
	if s.agent == nil {
		return ErrClusterAgentNotExpected
	}
	if err := s.agent.Set(clusterAgentIdentity(n)); err != nil {
		return err
	}
	for _, fn := range s.onClusterAgent {
		go fn(context.WithoutCancel(ctx))
	}
	return nil
}

// LoadClusterAgent points the API's cluster at the registered agent, if
// there is one; with none, cluster work waits for a registration.
func (s *NodeService) LoadClusterAgent(ctx context.Context) error {
	if s.agent == nil {
		return nil
	}
	var n db.Node
	err := s.db.WithContext(ctx).Where("cluster_agent_cert <> ''").First(&n).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.attachClusterAgent(ctx, &n)
}

// RevokeClusterAgent forgets a node's agent: the API's cluster stops reaching
// it at once, requests still open to it are cut, and a new agent may register.
func (s *NodeService) RevokeClusterAgent(ctx context.Context, nodeID uuid.UUID) error {
	res := s.db.WithContext(ctx).Model(&db.Node{}).
		Where("id = ? AND cluster_agent_cert <> ''", nodeID).
		Updates(map[string]any{"cluster_agent_cert": "", "cluster_agent_token": db.EncryptedString(""), "cluster_agent_port": 0})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNoClusterAgentOnNode
	}
	if s.agent != nil {
		s.agent.Clear()
	}
	return nil
}

// CreateClusterAgentToken mints the single-use token that registers the node
// holding the cluster's server, with its agent. Only an API that reaches its
// cluster through an agent mints one, and only while none is registered.
func (s *NodeService) CreateClusterAgentToken(ctx context.Context, orgID uuid.UUID, createdBy *uuid.UUID, label string, expiresAt *time.Time) (string, *db.NodeProvisioningToken, error) {
	if s.agent == nil {
		return "", nil, ErrClusterAgentNotExpected
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&db.Node{}).Where("cluster_agent_cert <> ''").Count(&n).Error; err != nil {
		return "", nil, err
	}
	if n > 0 {
		return "", nil, ErrClusterAgentRegistered
	}
	plaintext, row, err := s.CreateProvisioningTokenBy(ctx, orgID, createdBy, label, expiresAt)
	if err != nil {
		return "", nil, err
	}
	if err := s.db.WithContext(ctx).Model(row).Update("cluster_agent", true).Error; err != nil {
		return "", nil, err
	}
	row.ClusterAgent = true
	return plaintext, row, nil
}
