package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const projectFileName = ".meshploy"

type ProjectLink struct {
	ProjectID string `json:"project_id"`
}

// LoadProjectLink walks from cwd up to root looking for a .meshploy file.
func LoadProjectLink() (*ProjectLink, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	for {
		p := filepath.Join(dir, projectFileName)
		f, err := os.Open(p)
		if err == nil {
			defer f.Close()
			var link ProjectLink
			if err := json.NewDecoder(f).Decode(&link); err != nil {
				return nil, err
			}
			return &link, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return nil, nil
}

// SaveProjectLink writes a .meshploy file in the current directory.
func SaveProjectLink(projectID string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	p := filepath.Join(cwd, projectFileName)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(ProjectLink{ProjectID: projectID})
}

// RemoveProjectLink deletes the .meshploy file in the current directory.
func RemoveProjectLink() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(cwd, projectFileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
