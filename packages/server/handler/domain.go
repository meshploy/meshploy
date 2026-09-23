package handler

import (
	"context"
	"log"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

type DomainPathInput struct {
	OrgID    string `path:"orgId"`
	DomainID string `path:"domainId"`
}

type ListDomainsInput struct {
	OrgID string `path:"orgId"`
}

type ListDomainsOutput struct {
	Body []db.Domain
}

type GetDomainOutput struct {
	Body *db.Domain
}

type CreateDomainInput struct {
	OrgID string `path:"orgId"`
	Body  struct {
		BaseDomain string     `json:"base_domain" required:"true" doc:"The domain to add, e.g. example.com"`
		DNSMode    db.DNSMode `json:"dns_mode,omitempty" enum:"delegation,ondemand" doc:"How this domain's DNS is arranged. Defaults to delegation."`
	}
}

type SetDNSModeInput struct {
	OrgID    string `path:"orgId"`
	DomainID string `path:"domainId"`
	Body     struct {
		DNSMode db.DNSMode `json:"dns_mode" required:"true" enum:"delegation,ondemand"`
	}
}

type DomainCheckInput struct {
	Domain string `query:"domain" required:"true"`
}

func (h *Handler) registerDomainRoutes(api huma.API) {
	// Internal endpoint — called by Caddy before issuing an On-Demand TLS cert.
	// No auth: returns 200 if the hostname belongs to a verified custom-domain route.
	huma.Register(api, huma.Operation{
		OperationID: "domain-check",
		Method:      "GET",
		Path:        "/api/v1/internal/domain-check",
		Summary:     "Caddy ask endpoint for On-Demand TLS",
		Tags:        []string{"Internal"},
	}, h.DomainCheck)

	huma.Register(api, huma.Operation{
		OperationID: "list-domains",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains",
		Summary:     "List domains for an organization",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListDomains)

	huma.Register(api, huma.Operation{
		OperationID: "create-domain",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/domains",
		Summary:     "Add a base domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.CreateDomain)

	huma.Register(api, huma.Operation{
		OperationID: "verify-domain",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/verify",
		Summary:     "Check the ownership TXT record for a domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.VerifyDomain)

	huma.Register(api, huma.Operation{
		OperationID: "set-domain-dns-mode",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/dns-mode",
		Summary:     "Change how a domain's DNS is arranged",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.SetDomainDNSMode)

	huma.Register(api, huma.Operation{
		OperationID: "start-retiring-domain",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/retire",
		Summary:     "Start retiring a base domain: no new routes, everything already on it keeps serving",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.StartRetiringDomain)

	huma.Register(api, huma.Operation{
		OperationID: "stop-retiring-domain",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/retire",
		Summary:     "Stop retiring a base domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.StopRetiringDomain)

	huma.Register(api, huma.Operation{
		OperationID: "make-domain-primary",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/make-primary",
		Summary:     "Move the primary to this domain; the old one keeps serving the platform's names",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.MakeDomainPrimary)

	huma.Register(api, huma.Operation{
		OperationID: "list-domain-nodes",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/nodes",
		Summary:     "Nodes whose control connection goes through this domain's headscale name",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListDomainNodes)

	huma.Register(api, huma.Operation{
		OperationID: "mark-node-moved",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/nodes/{nodeId}/control-moved",
		Summary:     "Record that a node now reaches Headscale through the primary domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.MarkNodeMoved)

	huma.Register(api, huma.Operation{
		OperationID: "list-domain-integrations",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/integrations",
		Summary:     "Git provider registrations that still point at this domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListDomainIntegrations)

	huma.Register(api, huma.Operation{
		OperationID: "move-git-push-hooks",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/git-integrations/{id}/move-hooks",
		Summary:     "Move an integration's repository push hooks to the primary domain's API",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.MoveGitPushHooks)

	huma.Register(api, huma.Operation{
		OperationID: "mark-git-registration-updated",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/git-integrations/{id}/registration-updated",
		Summary:     "Record that a registration at the git provider was changed to the primary domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.MarkGitRegistrationUpdated)

	huma.Register(api, huma.Operation{
		OperationID: "list-domain-deploy-hooks",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/deploy-hooks",
		Summary:     "Services whose CI deploy webhook was last called through this domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListDomainDeployHooks)

	huma.Register(api, huma.Operation{
		OperationID: "forget-deploy-hook-call",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/services/{serviceId}/deploy-hook-call",
		Summary:     "Forget where a deploy webhook was last called from, for a CI job that no longer exists",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ForgetDeployHookCall)

	huma.Register(api, huma.Operation{
		OperationID: "list-domain-routes",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}/routes",
		Summary:     "List the hostnames served under a base domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListDomainRoutes)

	huma.Register(api, huma.Operation{
		OperationID: "list-custom-domains",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/custom-domains",
		Summary:     "List hostnames that belong to a route rather than a base domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListCustomDomains)

	huma.Register(api, huma.Operation{
		OperationID: "delete-domain",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}",
		Summary:     "Remove a base domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteDomain)

	huma.Register(api, huma.Operation{
		OperationID: "get-domain",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/domains/{domainId}",
		Summary:     "Get a domain",
		Tags:        []string{"Domains"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetDomain)
}

func (h *Handler) ListDomains(ctx context.Context, input *ListDomainsInput) (*ListDomainsOutput, error) {
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	domains, err := h.svc.Domains.List(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &ListDomainsOutput{Body: domains}, nil
}

func (h *Handler) GetDomain(ctx context.Context, input *DomainPathInput) (*GetDomainOutput, error) {
	_, orgID, domainID, err := h.checkOrgMemberAccess(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil {
		return nil, notFound(err)
	}
	if domain.OrganizationID != orgID {
		return nil, huma.Error404NotFound("domain not found")
	}
	return &GetDomainOutput{Body: domain}, nil
}

func (h *Handler) CreateDomain(ctx context.Context, input *CreateDomainInput) (*GetDomainOutput, error) {
	_, orgID, _, err := h.checkOrgAdminAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Create(ctx, orgID, input.Body.BaseDomain, input.Body.DNSMode)
	if err != nil {
		return nil, err
	}
	return &GetDomainOutput{Body: domain}, nil
}

func (h *Handler) VerifyDomain(ctx context.Context, input *DomainPathInput) (*GetDomainOutput, error) {
	domainID, err := h.domainOfOrg(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Verify(ctx, domainID)
	if err != nil {
		return nil, err
	}
	h.publishEdge(ctx, domain.OrganizationID)
	return &GetDomainOutput{Body: domain}, nil
}

func (h *Handler) SetDomainDNSMode(ctx context.Context, input *SetDNSModeInput) (*GetDomainOutput, error) {
	domainID, err := h.domainOfOrg(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.SetDNSMode(ctx, domainID, input.Body.DNSMode)
	if err != nil {
		return nil, err
	}
	h.publishEdge(ctx, domain.OrganizationID)
	return &GetDomainOutput{Body: domain}, nil
}

// The primary moving changes the edge - new platform blocks, Headscale's
// server_url - so it publishes. Nothing stops serving: the old primary's
// platform names are still in the set it publishes.
func (h *Handler) MakeDomainPrimary(ctx context.Context, input *DomainPathInput) (*GetDomainOutput, error) {
	domainID, err := h.domainOfOrg(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.SetPrimary(ctx, domainID)
	if err != nil {
		return nil, err
	}
	h.publishEdge(ctx, domain.OrganizationID)
	return &GetDomainOutput{Body: domain}, nil
}

type DomainNodesOutput struct {
	Body []service.ControlNode
}

func (h *Handler) ListDomainNodes(ctx context.Context, input *DomainPathInput) (*DomainNodesOutput, error) {
	_, orgID, domainID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil || domain.OrganizationID != orgID {
		return nil, huma.Error404NotFound("domain not found")
	}
	nodes, err := h.svc.Domains.NodesOnDomain(ctx, orgID, domain)
	if err != nil {
		return nil, err
	}
	return &DomainNodesOutput{Body: nodes}, nil
}

type MarkNodeMovedOutput struct {
	Body *db.Node
}

func (h *Handler) MarkNodeMoved(ctx context.Context, input *NodePathInput) (*MarkNodeMovedOutput, error) {
	_, orgID, nodeID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.NodeID)
	if err != nil {
		return nil, err
	}
	node, err := h.svc.Domains.MarkNodeMoved(ctx, orgID, nodeID)
	if err != nil {
		return nil, err
	}
	return &MarkNodeMovedOutput{Body: node}, nil
}

type DomainIntegrationsOutput struct {
	Body []service.IntegrationRegistration
}

func (h *Handler) ListDomainIntegrations(ctx context.Context, input *DomainPathInput) (*DomainIntegrationsOutput, error) {
	_, orgID, domainID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil || domain.OrganizationID != orgID {
		return nil, huma.Error404NotFound("domain not found")
	}
	regs, err := h.svc.Domains.IntegrationsOnDomain(ctx, h.svc.GitIntegrations, orgID, domain)
	if err != nil {
		return nil, err
	}
	return &DomainIntegrationsOutput{Body: regs}, nil
}

type HookMoveOutput struct {
	Body *service.HookMoveResult
}

func (h *Handler) MoveGitPushHooks(ctx context.Context, input *GitIntegrationPathInput) (*HookMoveOutput, error) {
	_, orgID, id, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.ID)
	if err != nil {
		return nil, err
	}
	res, err := h.svc.GitIntegrations.MovePushHooks(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	return &HookMoveOutput{Body: res}, nil
}

type MarkRegistrationInput struct {
	OrgID string `path:"orgId"`
	ID    string `path:"id"`
	Body  struct {
		Kind     string `json:"kind" required:"true" enum:"github_app,oauth_redirect,push_hooks"`
		DomainID string `json:"domain_id" required:"true" doc:"The domain the registration pointed at"`
	}
}

func (h *Handler) MarkGitRegistrationUpdated(ctx context.Context, input *MarkRegistrationInput) (*struct{}, error) {
	_, orgID, id, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.ID)
	if err != nil {
		return nil, err
	}
	domainID, err := parseUUID(input.Body.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil || domain.OrganizationID != orgID {
		return nil, huma.Error404NotFound("domain not found")
	}
	if _, err := h.svc.GitIntegrations.MarkRegistrationUpdated(ctx, orgID, id, input.Body.Kind, domain.BaseDomain); err != nil {
		return nil, err
	}
	return &struct{}{}, nil
}

type DomainDeployHooksOutput struct {
	Body []service.DeployHook
}

func (h *Handler) ListDomainDeployHooks(ctx context.Context, input *DomainPathInput) (*DomainDeployHooksOutput, error) {
	_, orgID, domainID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil || domain.OrganizationID != orgID {
		return nil, huma.Error404NotFound("domain not found")
	}
	hooks, err := h.svc.Domains.DeployHooksOnDomain(ctx, orgID, domain)
	if err != nil {
		return nil, err
	}
	return &DomainDeployHooksOutput{Body: hooks}, nil
}

type ServiceOrgPathInput struct {
	OrgID     string `path:"orgId"`
	ServiceID string `path:"serviceId"`
}

func (h *Handler) ForgetDeployHookCall(ctx context.Context, input *ServiceOrgPathInput) (*struct{}, error) {
	_, orgID, serviceID, err := h.checkOrgAdminAccess(ctx, input.OrgID, input.ServiceID)
	if err != nil {
		return nil, err
	}
	if err := h.svc.Domains.ForgetDeployHookCall(ctx, orgID, serviceID); err != nil {
		return nil, err
	}
	return &struct{}{}, nil
}

// Retiring changes nothing at the edge - a retiring domain is served exactly
// as before - so neither of these publishes a new domain set.
func (h *Handler) StartRetiringDomain(ctx context.Context, input *DomainPathInput) (*GetDomainOutput, error) {
	domainID, err := h.domainOfOrg(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.StartRetiring(ctx, domainID)
	if err != nil {
		return nil, err
	}
	return &GetDomainOutput{Body: domain}, nil
}

func (h *Handler) StopRetiringDomain(ctx context.Context, input *DomainPathInput) (*GetDomainOutput, error) {
	domainID, err := h.domainOfOrg(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.StopRetiring(ctx, domainID)
	if err != nil {
		return nil, err
	}
	return &GetDomainOutput{Body: domain}, nil
}

func (h *Handler) DeleteDomain(ctx context.Context, input *DomainPathInput) (*struct{}, error) {
	domainID, err := h.domainOfOrg(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil {
		return nil, notFound(err)
	}
	if err := h.svc.Domains.Delete(ctx, domainID); err != nil {
		return nil, err
	}
	h.publishEdge(ctx, domain.OrganizationID)
	return &struct{}{}, nil
}

// publishEdge records the new domain set and asks the host agent to serve it.
//
// Deliberately not fatal. The domain change itself has happened and is correct;
// failing the request because a host-side step did not run would report a
// change that did take place as one that did not, and leave the caller with
// nothing to do about it. The next publish sends the whole set again, so
// nothing is lost by one not landing.
func (h *Handler) publishEdge(ctx context.Context, orgID uuid.UUID) {
	by, _ := requireUser(ctx)
	if err := h.svc.System.PublishEdge(ctx, orgID, by); err != nil {
		log.Printf("warning: could not ask the host agent to serve the new domain set: %v", err)
	}
}

type DomainRoutesOutput struct {
	Body []service.DomainRoute
}

func (h *Handler) ListDomainRoutes(ctx context.Context, input *DomainPathInput) (*DomainRoutesOutput, error) {
	// A member, not an admin: this is a read, and every member can already see
	// the routes it lists on their project pages.
	_, orgID, domainID, err := h.checkOrgMemberAccess(ctx, input.OrgID, input.DomainID)
	if err != nil {
		return nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil {
		return nil, notFound(err)
	}
	if domain.OrganizationID != orgID {
		return nil, huma.Error404NotFound("domain not found")
	}
	rows, err := h.svc.Domains.RoutesOnDomain(ctx, orgID, domainID)
	if err != nil {
		return nil, err
	}
	return &DomainRoutesOutput{Body: rows}, nil
}

func (h *Handler) ListCustomDomains(ctx context.Context, input *ListDomainsInput) (*DomainRoutesOutput, error) {
	_, orgID, _, err := h.checkOrgMemberAccess(ctx, input.OrgID, "")
	if err != nil {
		return nil, err
	}
	rows, err := h.svc.Domains.CustomDomains(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return &DomainRoutesOutput{Body: rows}, nil
}

// domainOfOrg resolves a domain id for an org admin, refusing one that belongs
// to another org with the same 404 a missing one gets. Domains are globally
// unique, so a distinguishable error would say whether somebody else holds a
// name.
func (h *Handler) domainOfOrg(ctx context.Context, orgIDStr, domainIDStr string) (uuid.UUID, error) {
	_, orgID, domainID, err := h.checkOrgAdminAccess(ctx, orgIDStr, domainIDStr)
	if err != nil {
		return uuid.Nil, err
	}
	domain, err := h.svc.Domains.Get(ctx, domainID)
	if err != nil {
		return uuid.Nil, notFound(err)
	}
	if domain.OrganizationID != orgID {
		return uuid.Nil, huma.Error404NotFound("domain not found")
	}
	return domainID, nil
}

// DomainCheck is called by Caddy's on_demand_tls ask mechanism before it issues
// a TLS cert for an unknown hostname. Returns 200 only for verified custom domains.
func (h *Handler) DomainCheck(ctx context.Context, input *DomainCheckInput) (*struct{}, error) {
	if !h.svc.Routes.IsCustomDomainVerified(ctx, input.Domain) {
		return nil, huma.Error403Forbidden("domain not verified")
	}
	return &struct{}{}, nil
}
