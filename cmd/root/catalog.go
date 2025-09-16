package root

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/docker/cagent/pkg/telemetry"
	"github.com/spf13/cobra"
)

// NewCatalogCmd creates the catalog command with its subcommands
func NewCatalogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "catalog",
		Short: "Manage the agent catalog",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.AddCommand(newCatalogListCmd())

	return cmd
}

func newCatalogListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [org]",
		Short: "List catalog entries",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Track catalog list with "list" as the first argument for telemetry
			telemetryArgs := append([]string{"list"}, args...)
			telemetry.TrackCommand("catalog", telemetryArgs)

			var org string
			if len(args) == 0 {
				org = "agentcatalog"
			} else {
				org = args[0]
			}
			return listCatalog(cmd.Context(), org)
		},
	}

	return cmd
}

type hubRepoList struct {
	Count    int     `json:"count"`
	Next     *string `json:"next"`
	Previous *string `json:"previous"`
	Results  []struct {
		Name        string `json:"name"`
		Namespace   string `json:"namespace"`
		Description string `json:"description"`
		IsPrivate   bool   `json:"is_private"`
	} `json:"results"`
}

type hubRepo struct {
	Namespace   string
	Name        string
	Description string
	IsPrivate   bool
}

func fetchHubRepos(ctx context.Context, org string) ([]hubRepo, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/?page_size=100", org)

	var repos []hubRepo
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("Docker Hub API request failed: %s", resp.Status)
		}

		var page hubRepoList
		if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
			_ = resp.Body.Close()
			return nil, err
		}
		_ = resp.Body.Close()

		for _, r := range page.Results {
			ns := r.Namespace
			if ns == "" {
				ns = org
			}
			repos = append(repos, hubRepo{
				Namespace:   ns,
				Name:        r.Name,
				Description: r.Description,
				IsPrivate:   r.IsPrivate,
			})
		}

		if page.Next == nil || *page.Next == "" {
			break
		}
		url = *page.Next
	}

	return repos, nil
}

func listCatalog(ctx context.Context, org string) error {
	repos, err := fetchHubRepos(ctx, org)
	if err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	defer func() { _ = w.Flush() }()

	fmt.Fprintf(w, "NAME\tDESCRIPTION\n")

	for _, r := range repos {
		fullName := fmt.Sprintf("%s/%s", r.Namespace, r.Name)
		desc := strings.ReplaceAll(r.Description, "\n", " ")
		desc = strings.ReplaceAll(desc, "\t", " ")
		fmt.Fprintf(w, "%s\t%s\n", fullName, desc)
	}

	return nil
}
