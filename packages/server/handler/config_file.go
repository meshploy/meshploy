package handler

import (
	"context"

	"github.com/danielgtaylor/huma/v2"
	db "github.com/meshploy/packages/db"
	"github.com/meshploy/packages/server/service"
)

// Config files are gated on the project, not on a resource type of their own.
//
// ResourceType covers what gets delegated individually — a service, a job, a
// route. Nobody hands someone a single config file, and variable groups and
// volumes are already project-gated for the same reason.
func (h *Handler) registerConfigFileRoutes(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-config-files",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/config-files",
		Summary:     "List the project's config files",
		Tags:        []string{"ConfigFiles"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.ListConfigFiles)

	huma.Register(api, huma.Operation{
		OperationID: "get-config-file",
		Method:      "GET",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/config-files/{fileId}",
		Summary:     "Get a config file and the services mounting it",
		Tags:        []string{"ConfigFiles"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.GetConfigFile)

	huma.Register(api, huma.Operation{
		OperationID:   "create-config-file",
		Method:        "POST",
		Path:          "/api/v1/orgs/{orgId}/projects/{projectId}/config-files",
		Summary:       "Create a config file",
		Tags:          []string{"ConfigFiles"},
		Security:      []map[string][]string{{"bearer": {}}},
		DefaultStatus: 201,
	}, h.CreateConfigFile)

	huma.Register(api, huma.Operation{
		OperationID: "update-config-file",
		Method:      "PATCH",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/config-files/{fileId}",
		Summary:     "Replace a config file's content, re-applying every service using it",
		Tags:        []string{"ConfigFiles"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.UpdateConfigFile)

	huma.Register(api, huma.Operation{
		OperationID: "delete-config-file",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/config-files/{fileId}",
		Summary:     "Delete a config file that no service mounts",
		Tags:        []string{"ConfigFiles"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DeleteConfigFile)

	huma.Register(api, huma.Operation{
		OperationID: "attach-config-file",
		Method:      "POST",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/config-files/{fileId}/attach/{serviceId}",
		Summary:     "Mount a config file into a service",
		Tags:        []string{"ConfigFiles"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.AttachConfigFile)

	huma.Register(api, huma.Operation{
		OperationID: "detach-config-file",
		Method:      "DELETE",
		Path:        "/api/v1/orgs/{orgId}/projects/{projectId}/config-files/{fileId}/attach/{serviceId}",
		Summary:     "Unmount a config file from a service",
		Tags:        []string{"ConfigFiles"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, h.DetachConfigFile)
}

// configFileDTO never carries content. The value is write-only, as a variable
// group's values are: these hold credentials, and a list endpoint is the last
// place they should appear.
type configFileDTO struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	StackID   *string  `json:"stack_id"`
	Size      int      `json:"size"`
	Services  []string `json:"services"`
	CreatedAt string   `json:"created_at"`
}

// configFileServiceRef carries the id as well as the name, so the detail page
// can link to each service rather than only naming it. The list keeps plain
// names: it renders a count, and does not need the ids for every row.
type configFileServiceRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// configFileDetailDTO repeats configFileDTO's fields rather than embedding it,
// and that is load-bearing rather than sloppy.
//
// Huma builds the response schema with its own reflection, and getFields skips
// any field failing IsExported() BEFORE it looks at whether the field is
// anonymous (huma/v2 schema.go). An embedded *unexported* type is therefore
// dropped whole -- encoding/json promotes its fields, Huma does not, and the
// response silently arrives with only the outer fields. That shipped: the
// detail page rendered a config file with no name, no path and no size while
// attached_services and updated_at came through fine.
//
// Embedding an exported type would also work. Spelling the fields out keeps
// every DTO in this file unexported and makes the wire shape readable in one
// place, which is worth more than the seven duplicated lines.
type configFileDetailDTO struct {
	ID               string                 `json:"id"`
	Name             string                 `json:"name"`
	Path             string                 `json:"path"`
	StackID          *string                `json:"stack_id"`
	Size             int                    `json:"size"`
	Services         []string               `json:"services"`
	CreatedAt        string                 `json:"created_at"`
	AttachedServices []configFileServiceRef `json:"attached_services"`
	UpdatedAt        string                 `json:"updated_at"`
}

type ConfigFileListInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
}

type ConfigFileListOutput struct {
	Body struct {
		Files []configFileDTO `json:"files"`
	}
}

func (h *Handler) ListConfigFiles(ctx context.Context, input *ConfigFileListInput) (*ConfigFileListOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	files, err := h.svc.ConfigFiles.List(ctx, projectID)
	if err != nil {
		return nil, huma.Error500InternalServerError(err.Error())
	}
	out := &ConfigFileListOutput{}
	out.Body.Files = make([]configFileDTO, 0, len(files))
	for i := range files {
		f := &files[i]
		names := []string{}
		if attached, err := h.svc.ConfigFiles.AttachedServices(ctx, f.ID); err == nil {
			for j := range attached {
				names = append(names, attached[j].Name)
			}
		}
		var stackID *string
		if f.StackID != nil {
			s := f.StackID.String()
			stackID = &s
		}
		out.Body.Files = append(out.Body.Files, configFileDTO{
			ID:        f.ID.String(),
			Name:      f.Name,
			Path:      f.Path,
			StackID:   stackID,
			Size:      len(string(f.Content)),
			Services:  names,
			CreatedAt: f.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return out, nil
}

type ConfigFileGetInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	FileID    string `path:"fileId"`
}

type ConfigFileGetOutput struct {
	Body configFileDetailDTO
}

func (h *Handler) GetConfigFile(ctx context.Context, input *ConfigFileGetInput) (*ConfigFileGetOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionView, "")
	if err != nil {
		return nil, err
	}
	fileID, err := parseUUID(input.FileID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid config file id")
	}
	f, err := h.svc.ConfigFiles.GetForProject(ctx, projectID, fileID)
	if err != nil {
		return nil, huma.Error404NotFound("config file not found")
	}

	names := []string{}
	refs := []configFileServiceRef{}
	if attached, aerr := h.svc.ConfigFiles.AttachedServices(ctx, f.ID); aerr == nil {
		for i := range attached {
			names = append(names, attached[i].Name)
			refs = append(refs, configFileServiceRef{ID: attached[i].ID.String(), Name: attached[i].Name})
		}
	}
	var stackID *string
	if f.StackID != nil {
		sid := f.StackID.String()
		stackID = &sid
	}

	out := &ConfigFileGetOutput{}
	out.Body = configFileDetailDTO{
		ID:               f.ID.String(),
		Name:             f.Name,
		Path:             f.Path,
		StackID:          stackID,
		Size:             len(string(f.Content)),
		Services:         names,
		CreatedAt:        f.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		AttachedServices: refs,
		UpdatedAt:        f.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	return out, nil
}

type ConfigFileBody struct {
	Name    string `json:"name"    doc:"Display name, e.g. \"zot config\""`
	Path    string `json:"path"    doc:"Absolute path inside the container, e.g. /etc/zot/config.json"`
	Content string `json:"content" doc:"File contents. Stored encrypted and never returned."`
}

type CreateConfigFileInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	Body      ConfigFileBody
}

type ConfigFileOutput struct {
	Body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Path string `json:"path"`
	}
}

func (h *Handler) CreateConfigFile(ctx context.Context, input *CreateConfigFileInput) (*ConfigFileOutput, error) {
	_, _, projectID, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionCreate, "")
	if err != nil {
		return nil, err
	}
	f, err := h.svc.ConfigFiles.Create(ctx, projectID, service.CreateConfigFileInput{
		Name: input.Body.Name, Path: input.Body.Path, Content: input.Body.Content,
	})
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &ConfigFileOutput{}
	out.Body.ID, out.Body.Name, out.Body.Path = f.ID.String(), f.Name, f.Path
	return out, nil
}

type UpdateConfigFileInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	FileID    string `path:"fileId"`
	Body      ConfigFileBody
}

func (h *Handler) UpdateConfigFile(ctx context.Context, input *UpdateConfigFileInput) (*ConfigFileOutput, error) {
	if _, _, _, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, ""); err != nil {
		return nil, err
	}
	fileID, err := parseUUID(input.FileID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid config file id")
	}
	f, err := h.svc.ConfigFiles.Update(ctx, fileID, service.CreateConfigFileInput{
		Name: input.Body.Name, Path: input.Body.Path, Content: input.Body.Content,
	})
	if err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &ConfigFileOutput{}
	out.Body.ID, out.Body.Name, out.Body.Path = f.ID.String(), f.Name, f.Path
	return out, nil
}

type ConfigFileIDInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	FileID    string `path:"fileId"`
}

type ConfigFileMessageOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

func (h *Handler) DeleteConfigFile(ctx context.Context, input *ConfigFileIDInput) (*ConfigFileMessageOutput, error) {
	if _, _, _, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionDelete, ""); err != nil {
		return nil, err
	}
	fileID, err := parseUUID(input.FileID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid config file id")
	}
	if err := h.svc.ConfigFiles.Delete(ctx, fileID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &ConfigFileMessageOutput{}
	out.Body.Message = "config file deleted"
	return out, nil
}

type ConfigFileAttachInput struct {
	OrgID     string `path:"orgId"`
	ProjectID string `path:"projectId"`
	FileID    string `path:"fileId"`
	ServiceID string `path:"serviceId"`
}

func (h *Handler) AttachConfigFile(ctx context.Context, input *ConfigFileAttachInput) (*ConfigFileMessageOutput, error) {
	if _, _, _, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, ""); err != nil {
		return nil, err
	}
	fileID, err := parseUUID(input.FileID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid config file id")
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid service id")
	}
	if err := h.svc.ConfigFiles.Attach(ctx, fileID, serviceID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &ConfigFileMessageOutput{}
	out.Body.Message = "config file attached"
	return out, nil
}

func (h *Handler) DetachConfigFile(ctx context.Context, input *ConfigFileAttachInput) (*ConfigFileMessageOutput, error) {
	if _, _, _, _, err := h.checkAccess(ctx, input.OrgID, input.ProjectID, db.ResourceProject, db.ActionUpdate, ""); err != nil {
		return nil, err
	}
	fileID, err := parseUUID(input.FileID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid config file id")
	}
	serviceID, err := parseUUID(input.ServiceID)
	if err != nil {
		return nil, huma.Error400BadRequest("invalid service id")
	}
	if err := h.svc.ConfigFiles.Detach(ctx, fileID, serviceID); err != nil {
		return nil, huma.Error400BadRequest(err.Error())
	}
	out := &ConfigFileMessageOutput{}
	out.Body.Message = "config file detached"
	return out, nil
}
