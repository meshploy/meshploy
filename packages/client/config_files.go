package client

// ConfigFile is a file projected into a service at a path.
//
// Content is absent by design: the API stores it encrypted and never returns
// it, so an agent can see that a file exists, where it mounts and who uses it,
// without the credential inside travelling any further than it must.
type ConfigFile struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Path     string   `json:"path"`
	StackID  *string  `json:"stack_id"`
	Size     int      `json:"size"`
	Services []string `json:"services"`
}

func cfBase(orgID, projectID string) string {
	return "/api/v1/orgs/" + orgID + "/projects/" + projectID + "/config-files"
}

func (c *Client) ListConfigFiles(orgID, projectID string) ([]ConfigFile, error) {
	resp, err := c.do("GET", cfBase(orgID, projectID), nil)
	if err != nil {
		return nil, err
	}
	out, err := decode[struct {
		Files []ConfigFile `json:"files"`
	}](resp)
	if err != nil {
		return nil, err
	}
	return out.Files, nil
}

func (c *Client) CreateConfigFile(orgID, projectID, name, path, content string) (*ConfigFile, error) {
	type body struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	resp, err := c.do("POST", cfBase(orgID, projectID), body{Name: name, Path: path, Content: content})
	if err != nil {
		return nil, err
	}
	f, err := decode[ConfigFile](resp)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (c *Client) UpdateConfigFile(orgID, projectID, fileID, name, path, content string) (*ConfigFile, error) {
	type body struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	resp, err := c.do("PATCH", cfBase(orgID, projectID)+"/"+fileID, body{Name: name, Path: path, Content: content})
	if err != nil {
		return nil, err
	}
	f, err := decode[ConfigFile](resp)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (c *Client) DeleteConfigFile(orgID, projectID, fileID string) error {
	_, err := c.do("DELETE", cfBase(orgID, projectID)+"/"+fileID, nil)
	return err
}

func (c *Client) AttachConfigFile(orgID, projectID, fileID, serviceID string) error {
	_, err := c.do("POST", cfBase(orgID, projectID)+"/"+fileID+"/attach/"+serviceID, nil)
	return err
}

func (c *Client) DetachConfigFile(orgID, projectID, fileID, serviceID string) error {
	_, err := c.do("DELETE", cfBase(orgID, projectID)+"/"+fileID+"/attach/"+serviceID, nil)
	return err
}
