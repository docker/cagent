package gateway

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/goccy/go-yaml"
)

const DockerCatalogURL = "https://desktop.docker.com/mcp/catalog/v3/catalog.yaml"

func ParseServerRef(ref string) string {
	return strings.TrimPrefix(ref, "docker:")
}

func RequiredEnvVars(ctx context.Context, serverName, catalogURL string) ([]Secret, error) {
	catalog, err := readCatalog(ctx, catalogURL)
	if err != nil {
		return nil, err
	}

	server, ok := catalog[serverName]
	if !ok {
		return nil, fmt.Errorf("MCP server %q not found in catalog %q", serverName, catalogURL)
	}

	return server.Secrets, nil
}

// TODO(dga): cache the catalog.
func readCatalog(ctx context.Context, url string) (Catalog, error) {
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

	var topLevel topLevel
	if err := yaml.Unmarshal(buf, &topLevel); err != nil {
		return nil, err
	}

	return topLevel.Catalog, nil
}
