package oci

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/stretchr/testify/assert/yaml"
)

const DockerCatalogURL = "https://desktop.docker.com/mcp/catalog/v2/catalog.yaml"

type topLevel struct {
	Catalog Catalog `json:"registry" yaml:"registry"`
}

type Catalog map[string]MCPServer

type MCPServer struct {
	Image   string      `json:"image,omitempty" yaml:"image,omitempty"`
	Command []string    `json:"command,omitempty" yaml:"command,omitempty"`
	Secrets []Secret    `json:"secrets,omitempty" yaml:"secrets,omitempty"`
	Env     []Env       `json:"env,omitempty" yaml:"env,omitempty"`
	Config  []MCPConfig `json:"config,omitempty" yaml:"config,omitempty"`
	Volumes []string    `json:"volumes,omitempty" yaml:"volumes,omitempty"`
}

type Secret struct {
	Name string `json:"name" yaml:"name"`
	Env  string `json:"env" yaml:"env"`
}

type Env struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type MCPConfig struct {
	Properties map[string]Property `json:"properties,omitempty" yaml:"properties,omitempty"`
}

type Property struct {
	Type string `json:"type" yaml:"type"`
}

var (
	fetchCatalogOnce sync.Once
	catalogBuf       []byte
	catalogErr       error
)

// readCatalog can be changed by tests
var readCatalog = func(ctx context.Context) (Catalog, error) {
	buf, err := fetchRemoteCatalog(ctx)
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		buf, err = os.ReadFile(filepath.Join(home, ".docker", "mcp", "catalogs", "docker-mcp.yaml"))
		if err != nil {
			return nil, err
		}
	}

	var topLevel topLevel
	if err := yaml.Unmarshal(buf, &topLevel); err != nil {
		return nil, err
	}

	return topLevel.Catalog, nil
}

func fetchRemoteCatalog(ctx context.Context) ([]byte, error) {
	fetchCatalogOnce.Do(func() {
		catalogBuf, catalogErr = doFetchRemoteCatalog(ctx)
		if catalogErr != nil {
			fmt.Fprintln(os.Stderr, "WARNING: failed to fetch catalog:", catalogErr)
		}
	})
	return catalogBuf, catalogErr
}

func doFetchRemoteCatalog(ctx context.Context) ([]byte, error) {
	url := DockerCatalogURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch URL: %s, status: %s", url, resp.Status)
	}

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return buf, nil
}
