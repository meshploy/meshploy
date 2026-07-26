package client

type Permission struct {
	ID           string `json:"id"`
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Action       string `json:"action"`
	ResourceName string `json:"resource_name,omitempty"`
}

type PermissionsWithUser struct {
	UserID    string `json:"user_id"`
	UserName  string `json:"user_name"`
	UserEmail string `json:"user_email"`
	Action    string `json:"action"`
}

type GrantPermissionBody struct {
	ResourceType string `json:"resource_type"`
	ResourceID   string `json:"resource_id"`
	Action       string `json:"action"`
}

func (c *Client) ListMemberPermissions(orgID, userID string) ([]Permission, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/members/"+userID+"/permissions", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Permission](resp)
}

func (c *Client) GrantPermission(orgID, userID string, body GrantPermissionBody) error {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/members/"+userID+"/permissions", body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) RevokePermission(orgID, userID string, body GrantPermissionBody) error {
	resp, err := c.do("DELETE", "/api/v1/orgs/"+orgID+"/members/"+userID+"/permissions", body)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// resourceTypePath maps the resource type name to its URL segment.
// The API uses resource-type-scoped paths: /orgs/{orgId}/{type}s/{resourceId}/permissions
func resourceTypePath(resourceType string) string {
	switch resourceType {
	case "project":
		return "projects"
	case "service":
		return "services"
	case "stack":
		return "stacks"
	case "job":
		return "jobs"
	default:
		return resourceType + "s"
	}
}

func (c *Client) ListResourcePermissions(orgID, resourceType, resourceID string) ([]PermissionsWithUser, error) {
	path := "/api/v1/orgs/" + orgID + "/" + resourceTypePath(resourceType) + "/" + resourceID + "/permissions"
	resp, err := c.do("GET", path, nil)
	if err != nil {
		return nil, err
	}
	return decode[[]PermissionsWithUser](resp)
}
