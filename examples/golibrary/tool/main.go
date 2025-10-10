package main

import (
	"context"
	"encoding/json"
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
	"github.com/docker/cagent/pkg/tools"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		fmt.Println(err)
	}
}

type AddNumbersArgs struct {
	A int `json:"a"`
	B int `json:"b"`
}

func addNumbers(_ context.Context, toolCall tools.ToolCall) (*tools.ToolCallResult, error) {
	var p AddNumbersArgs
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &p); err != nil {
		return nil, err
	}

	fmt.Println("Adding numbers", p.A, p.B)

	return &tools.ToolCallResult{
		Output: fmt.Sprintf("%d", p.A+p.B),
	}, nil
}

func run(ctx context.Context) error {
	llm, err := openai.NewClient(
		ctx,
		&latest.ModelConfig{
			Provider: "openai",
			Model:    "gpt-4o",
		},
		environment.NewDefaultProvider(ctx),
	)
	if err != nil {
		return err
	}

	toolAddNumbers := tools.Tool{
		Name:        "add",
		Description: "Add two numbers",
		Parameters:  tools.MustSchemaFor[AddNumbersArgs](),
		Handler:     addNumbers,
	}

	calculator := agent.New(
		"root",
		"You are a human, with feelings and emotions.",
		agent.WithModel(llm),
		agent.WithTools(toolAddNumbers),
	)

	calculatorTeam := team.New(team.WithAgents(calculator))

	rt, err := runtime.New(calculatorTeam)
	if err != nil {
		return err
	}

	sess := session.New(session.WithUserMessage("", "What is 1 + 2?"))

	messages, err := rt.Run(ctx, sess)
	if err != nil {
		return err
	}

	fmt.Println(messages[len(messages)-1].Message.Content)
	return nil
}
