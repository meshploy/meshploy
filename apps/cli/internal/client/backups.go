package client

type BackupConfig struct {
	ID                   string `json:"id"`
	ServiceID            string `json:"service_id"`
	StorageIntegrationID string `json:"storage_integration_id"`
	Schedule             string `json:"schedule"`
	RetentionDays        int    `json:"retention_days"`
	PathPrefix           string `json:"path_prefix"`
	Enabled              bool   `json:"enabled"`
}

type CreateBackupConfigBody struct {
	StorageIntegrationID string `json:"storage_integration_id"`
	Schedule             string `json:"schedule"`
	RetentionDays        int    `json:"retention_days,omitempty"`
	PathPrefix           string `json:"path_prefix,omitempty"`
}

type UpdateBackupConfigBody struct {
	Schedule      *string `json:"schedule,omitempty"`
	RetentionDays *int    `json:"retention_days,omitempty"`
	PathPrefix    *string `json:"path_prefix,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
}

type BackupObject struct {
	Key          string `json:"key"`
	Size         int64  `json:"size"`
	LastModified string `json:"last_modified"`
}

type SystemBackupConfig struct {
	ID                   string `json:"id"`
	StorageIntegrationID string `json:"storage_integration_id"`
	Schedule             string `json:"schedule"`
	RetentionDays        int    `json:"retention_days"`
	PathPrefix           string `json:"path_prefix"`
	Enabled              bool   `json:"enabled"`
}

type UpsertSystemBackupBody struct {
	StorageIntegrationID string `json:"storage_integration_id"`
	Schedule             string `json:"schedule"`
	RetentionDays        int    `json:"retention_days,omitempty"`
	PathPrefix           string `json:"path_prefix,omitempty"`
	Enabled              bool   `json:"enabled"`
}

func backupBase(orgID, projectID, serviceID string) string {
	return "/api/v1/orgs/" + orgID + "/projects/" + projectID + "/services/" + serviceID + "/backups"
}

func (c *Client) ListBackupConfigs(orgID, projectID, serviceID string) ([]BackupConfig, error) {
	resp, err := c.do("GET", backupBase(orgID, projectID, serviceID), nil)
	if err != nil {
		return nil, err
	}
	return decode[[]BackupConfig](resp)
}

func (c *Client) CreateBackupConfig(orgID, projectID, serviceID string, body CreateBackupConfigBody) (*BackupConfig, error) {
	resp, err := c.do("POST", backupBase(orgID, projectID, serviceID), body)
	if err != nil {
		return nil, err
	}
	return decodePtr[BackupConfig](resp)
}

func (c *Client) UpdateBackupConfig(orgID, projectID, serviceID, id string, body UpdateBackupConfigBody) (*BackupConfig, error) {
	resp, err := c.do("PATCH", backupBase(orgID, projectID, serviceID)+"/"+id, body)
	if err != nil {
		return nil, err
	}
	return decodePtr[BackupConfig](resp)
}

func (c *Client) DeleteBackupConfig(orgID, projectID, serviceID, id string) error {
	return c.doNoContent("DELETE", backupBase(orgID, projectID, serviceID)+"/"+id)
}

func (c *Client) TriggerBackup(orgID, projectID, serviceID, id string) error {
	return c.doNoContent("POST", backupBase(orgID, projectID, serviceID)+"/"+id+"/trigger")
}

func (c *Client) ListBackupObjects(orgID, projectID, serviceID, id string) ([]BackupObject, error) {
	resp, err := c.do("GET", backupBase(orgID, projectID, serviceID)+"/"+id+"/objects", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]BackupObject](resp)
}

func (c *Client) RestoreBackup(orgID, projectID, serviceID, id, key string) error {
	resp, err := c.do("POST", backupBase(orgID, projectID, serviceID)+"/"+id+"/restore", map[string]string{"key": key})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) GetSystemBackup(orgID string) (*SystemBackupConfig, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/system-backup", nil)
	if err != nil {
		return nil, err
	}
	return decodePtr[SystemBackupConfig](resp)
}

func (c *Client) UpsertSystemBackup(orgID string, body UpsertSystemBackupBody) (*SystemBackupConfig, error) {
	resp, err := c.do("PUT", "/api/v1/orgs/"+orgID+"/system-backup", body)
	if err != nil {
		return nil, err
	}
	return decodePtr[SystemBackupConfig](resp)
}

func (c *Client) TriggerSystemBackup(orgID string) error {
	return c.doNoContent("POST", "/api/v1/orgs/"+orgID+"/system-backup/trigger")
}

func (c *Client) ListSystemBackupObjects(orgID string) ([]BackupObject, error) {
	resp, err := c.do("GET", "/api/v1/orgs/"+orgID+"/system-backup/objects", nil)
	if err != nil {
		return nil, err
	}
	return decode[[]BackupObject](resp)
}

func (c *Client) RestoreSystemBackup(orgID, key string) error {
	resp, err := c.do("POST", "/api/v1/orgs/"+orgID+"/system-backup/restore", map[string]string{"key": key})
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
