package desktop

import (
	"context"
)

type DockerHubInfo struct {
	Email         string   `json:"email,omitempty"`
	Organizations []string `json:"organizations,omitempty"`
	PlanName      string   `json:"planName"`
}

func GetToken(ctx context.Context) string {
	var token string
	_ = ClientBackend.Get(ctx, "/registry/token", &token)
	return token
}
