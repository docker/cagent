package main

import (
	"context"
	"fmt"
	"os/signal"
	"syscall"

	"github.com/docker/cagent/pkg/agent"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Println(err)
	}
}

func run(ctx context.Context) error {
	llm, err := openai.NewClient(
		ctx,
		&latest.ModelConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
		environment.NewDefaultProvider(),
	)
	if err != nil {
		return err
	}

	human := agent.New(
		"root",
		"You are a human, with feelings and emotions.",
		agent.WithModel(llm),
		agent.WithDescription("A human."),
	)

	humanTeam := team.New(team.WithAgents(human))

	rt, err := runtime.New(humanTeam)
	if err != nil {
		return err
	}

	sess := session.New(session.WithUserMessage("", "How are you doing?"))

	messages, err := rt.Run(ctx, sess)
	if err != nil {
		return err
	}

	fmt.Println(messages[len(messages)-1].Message.Content)
	return nil
}
