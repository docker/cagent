package main

import (
	"context"
	"fmt"
	"log"

	"github.com/docker/cagent/pkg/agent"
	latest "github.com/docker/cagent/pkg/config/v2"
	"github.com/docker/cagent/pkg/environment"
	"github.com/docker/cagent/pkg/model/provider/openai"
	"github.com/docker/cagent/pkg/runtime"
	"github.com/docker/cagent/pkg/session"
	"github.com/docker/cagent/pkg/team"
	"github.com/docker/cagent/pkg/tools/builtin"
)

func main() {
	ctx := context.Background()

	llm, err := openai.NewClient(
		ctx,
		&latest.ModelConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
		environment.NewDefaultProvider(ctx))
	if err != nil {
		log.Fatal(err)
	}

	child := agent.New(
		"child",
		"You are a child, with a lot of energy.",
		agent.WithModel(llm),
		agent.WithDescription("A child."),
	)
	root := agent.New(
		"root",
		"You are a human, with feelings and emotions.",
		agent.WithModel(llm),
		agent.WithSubAgents(child),
		agent.WithToolSets(builtin.NewTransferTaskTool()),
	)
	rt, err := runtime.New(team.New(team.WithAgents(root, child)))
	if err != nil {
		log.Fatal(err)
	}

	sess := session.New(session.WithUserMessage("", "Ask your child how they are doing and tell me what they said"))

	messages, err := rt.Run(ctx, sess)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(messages[len(messages)-1].Message.Content)
}
