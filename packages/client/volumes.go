package client

type Volume struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Slug      string  `json:"slug"`
	StorageGB int     `json:"storage_gb"`
	Status    string  `json:"status"`
	NodeID    *string `json:"node_id"` // nil = auto-schedule
	CreatedAt string  `json:"created_at"`
}

// VolumePlacement is where a volume's claim actually is, which can differ from
// the requested NodeID: node-local storage cannot move once bound.
type VolumePlacement struct {
	Exists bool   `json:"exists"`
	Phase  string `json:"phase"`
	Bound  bool   `json:"bound"`
	Node   string `json:"node"`
}

type VolumeMount struct {
	ID        string `json:"id"`
	VolumeID  string `json:"volume_id"`
	ServiceID string `json:"service_id"`
	MountPath string `json:"mount_path"`
}

type CreateVolumeBody struct {
	Name      string `json:"name"`
	StorageGB int    `json:"storage_gb"`
	// NodeID pins provisioning to a node. Omit to auto-schedule.
	NodeID *string `json:"node_id,omitempty"`
}

// SetVolumeNodeBody moves an unbound volume to a different node. nil clears the
// pin and returns it to auto-schedule.
type SetVolumeNodeBody struct {
	NodeID *string `json:"node_id"`
}

type AttachVolumeBody struct {
	ServiceID string `json:"service_id"`
	MountPath string `json:"mount_path"`
}

func (c *Client) ListVolumes(orgID, projectID string) ([]Volume, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]Volume](resp)
}

func (c *Client) CreateVolume(orgID, projectID string, body CreateVolumeBody) (*Volume, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Volume](resp)
}

func (c *Client) GetVolume(orgID, projectID, volumeID string) (*Volume, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes/"+volumeID, nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[Volume](resp)
}

// SetVolumeNode pins a volume to a node, or clears the pin when body.NodeID is
// nil. Fails if the claim is already bound elsewhere.
func (c *Client) SetVolumeNode(orgID, projectID, volumeID string, body SetVolumeNodeBody) (*Volume, error) {
	resp, err := c.do("PUT", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes/"+volumeID+"/node", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[Volume](resp)
}

// GetVolumePlacement reports where the claim is bound or pinned.
func (c *Client) GetVolumePlacement(orgID, projectID, volumeID string) (*VolumePlacement, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes/"+volumeID+"/placement", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[VolumePlacement](resp)
}

func (c *Client) DeleteVolume(orgID, projectID, volumeID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes/"+volumeID)
}

func (c *Client) AttachVolume(orgID, projectID, volumeID string, body AttachVolumeBody) (*VolumeMount, error) {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes/"+volumeID+"/mounts", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[VolumeMount](resp)
}

func (c *Client) DetachVolume(orgID, projectID, volumeID, mountID string) error {
	return c.doNoContent("DELETE", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes/"+volumeID+"/mounts/"+mountID)
}

func (c *Client) ListVolumeMounts(orgID, projectID, volumeID string) ([]VolumeMount, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/projects/"+projectID+"/volumes/"+volumeID+"/mounts", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]VolumeMount](resp)
}

func (c *Client) GetVolumeByName(orgID, projectID, ref string) (*Volume, error) {
	volumes, err := c.ListVolumes(orgID, projectID)
	if err != nil {
		return nil, err
	}
	for i, v := range volumes {
		if v.ID == ref || v.Name == ref || v.Slug == ref {
			return &volumes[i], nil
		}
	}
	return nil, ErrNotFound("volume", ref)
}
