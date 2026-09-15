package client

// TemplateVariable is one deploy-time input a template declares. Only the
// declaration travels over the API: a prompted value is supplied on deploy, a
// generated one is made by the server, and neither is ever read back.
type TemplateVariable struct {
	Key      string `json:"key"`
	Prompt   string `json:"prompt,omitempty"`
	Required bool   `json:"required,omitempty"`
	Generate string `json:"generate,omitempty"`
}

type Template struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Category    string             `json:"category"`
	Version     string             `json:"version"`
	Variables   []TemplateVariable `json:"variables"`
}

// TemplateDetail is a template plus the compose it deploys, so a caller can
// read what it will create before creating it, or edit it first.
type TemplateDetail struct {
	Manifest *Template `json:"manifest"`
	Compose  string    `json:"compose"`
}

// DeployTemplateBody supplies what the template asks for. Spec overrides the
// template's own compose when a caller has edited it.
type DeployTemplateBody struct {
	Spec         string            `json:"spec,omitempty"`
	PromptValues map[string]string `json:"prompt_values,omitempty"`
}

func (c *Client) ListTemplates() ([]Template, error) {
	resp, err := c.do("GET", "/api/v1/templates", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Template](resp)
}

func (c *Client) GetTemplate(templateID string) (*TemplateDetail, error) {
	resp, err := c.do("GET", "/api/v1/templates/"+templateID, nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[TemplateDetail](resp)
}

// DeployTemplate instantiates a template into a project: it creates the stack,
// reconciles it, and publishes a route for each service the template exposes.
func (c *Client) DeployTemplate(orgID, projectID, templateID string, body DeployTemplateBody) (*Stack, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/templates/"+templateID+"/deploy", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Stack](resp)
}
