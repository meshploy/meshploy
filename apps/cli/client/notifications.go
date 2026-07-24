package client

type NotificationChannel struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Config  map[string]string `json:"config"`
	Events  []string          `json:"events"`
	Enabled bool              `json:"enabled"`
}

type CreateNotificationBody struct {
	Name   string            `json:"name"`
	Type   string            `json:"type"`
	Config map[string]string `json:"config"`
	Events []string          `json:"events"`
}

type UpdateNotificationBody struct {
	Name    *string           `json:"name,omitempty"`
	Config  map[string]string `json:"config,omitempty"`
	Events  []string          `json:"events,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"`
}

func (c *Client) ListNotificationChannels(orgID string) ([]NotificationChannel, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/notification-channels", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]NotificationChannel](resp)
}

func (c *Client) CreateNotificationChannel(orgID string, body CreateNotificationBody) (*NotificationChannel, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/notification-channels", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[NotificationChannel](resp)
}

func (c *Client) UpdateNotificationChannel(orgID, id string, body UpdateNotificationBody) (*NotificationChannel, error) {
	resp, err := c.do("PUT", "/api/v1/orgs/"+orgID+"/notification-channels/"+id, body)
	if err != nil {
		return nil, err
	}
	return decodePtr[NotificationChannel](resp)
}

func (c *Client) DeleteNotificationChannel(orgID, id string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/notification-channels/"+id)
}
