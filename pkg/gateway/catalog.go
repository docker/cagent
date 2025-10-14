package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/docker/cagent/pkg/sync"
)

const DockerCatalogURL = "https://desktop.docker.com/mcp/catalog/v3/catalog.yaml"

func RequiredEnvVars(ctx context.Context, serverName string) ([]Secret, error) {
	server, err := ServerSpec(ctx, serverName)
	if err != nil {
		return nil, err
	}

	// TODO(dga): until the MCP Gateway supports oauth with cagent,
	// we ignore every secret listed on `remote` servers and assume
	// we can use oauth by connecting directly to the server's url.
	if server.Type == "remote" {
		return nil, nil
	}

	return server.Secrets, nil
}

func ServerSpec(_ context.Context, serverName string) (Server, error) {
	catalog, err := readCatalogOnce()
	if err != nil {
		return Server{}, fmt.Errorf("failed to fetch MCP catalog: %w", err)
	}

	server, ok := catalog[serverName]
	if !ok {
		return Server{}, fmt.Errorf("MCP server %q not found in MCP catalog", serverName)
	}

	return server, nil
}

// Read the MCP Catalog only once and cache the result.
var readCatalogOnce = sync.OnceErr(func() (Catalog, error) {
	// Use the JSON version because it's 3x time faster to parse than YAML.
	url := strings.Replace(DockerCatalogURL, ".yaml", ".json", 1)

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch URL: %s, status: %s", url, resp.Status)
	}

	var topLevel topLevel
	if err := json.NewDecoder(resp.Body).Decode(&topLevel); err != nil {
		return nil, err
	}

	return topLevel.Catalog, nil
})
