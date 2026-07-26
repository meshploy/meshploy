package client

type Volume struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Slug      string  `json:"slug"`
	StorageGB int     `json:"storage_gb"`
	Status    string  `json:"status"`
	CreatedAt string  `json:"created_at"`
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
