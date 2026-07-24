package client

import "fmt"

type VariableGroupItem struct {
	ID       string `json:"id"`
	GroupID  string `json:"group_id"`
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	IsSecret bool   `json:"is_secret"`
}

type VariableGroup struct {
	ID          string              `json:"id"`
	ProjectID   string              `json:"project_id"`
	ServiceID   *string             `json:"service_id,omitempty"`
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Items       []VariableGroupItem `json:"items"`
}

type CreateVariableGroupBody struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type UpdateVariableGroupBody struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

type UpsertVariableItemBody struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	IsSecret bool   `json:"is_secret"`
}

func vgBase(orgID, projectID string) string {
	return "/api/v1/orgs/" + orgID + "/projects/" + projectID + "/variable-groups"
}

func (c *Client) ListVariableGroups(orgID, projectID string) ([]VariableGroup, error) {
	resp, err := c.do("GET", vgBase(orgID, projectID), nil)
	if err != nil {
		return nil, err
	}
	return decode[[]VariableGroup](resp)
}

func (c *Client) CreateVariableGroup(orgID, projectID string, body CreateVariableGroupBody) (*VariableGroup, error) {
	resp, err := c.do("POST", vgBase(orgID, projectID), body)
	if err != nil {
		return nil, err
	}
	return decodePtr[VariableGroup](resp)
}

func (c *Client) GetVariableGroup(orgID, projectID, groupID string) (*VariableGroup, error) {
	resp, err := c.do("GET", vgBase(orgID, projectID)+"/"+groupID, nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[VariableGroup](resp)
}

func (c *Client) UpdateVariableGroup(orgID, projectID, groupID string, body UpdateVariableGroupBody) (*VariableGroup, error) {
	resp, err := c.do("PATCH", vgBase(orgID, projectID)+"/"+groupID, body)
	if err != nil {
		return nil, err
	}
	return decodePtr[VariableGroup](resp)
}

func (c *Client) DeleteVariableGroup(orgID, projectID, groupID string) error {
	return c.doNoContent("DELETE", vgBase(orgID, projectID)+"/"+groupID)
}

func (c *Client) UpsertVariableItem(orgID, projectID, groupID string, body UpsertVariableItemBody) (*VariableGroupItem, error) {
	resp, err := c.do("PUT", vgBase(orgID, projectID)+"/"+groupID+"/items", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[VariableGroupItem](resp)
}

func (c *Client) DeleteVariableItem(orgID, projectID, groupID, itemID string) error {
	return c.doNoContent("DELETE", vgBase(orgID, projectID)+"/"+groupID+"/items/"+itemID)
}

func (c *Client) ListServiceVariableGroups(orgID, projectID, serviceID string) ([]VariableGroup, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/variable-groups", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]VariableGroup](resp)
}

func (c *Client) AttachVariableGroup(orgID, projectID, serviceID, groupID string) error {
	type body struct {
		GroupID string `json:"group_id"`
	}
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/variable-groups", body{GroupID: groupID})
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) DetachVariableGroup(orgID, projectID, serviceID, groupID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/services/"+serviceID+"/variable-groups/"+groupID)
}

// GetVariableGroupByName resolves a variable group by ID or name within a project.
func (c *Client) GetVariableGroupByName(orgID, projectID, ref string) (*VariableGroup, error) {
	groups, err := c.ListVariableGroups(orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i, g := range groups {
		if g.ID == ref || g.Name == ref {
			return &groups[i], nil
		}
	}
	return nil, ErrNotFound("variable group", ref)
}
