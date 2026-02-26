package root

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/docker/cagent/pkg/chatgpt"
)

func newLoginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "login <provider>",
		Short:   "Authenticate with a model provider",
		Long:    "Authenticate with a model provider using OAuth. Currently supports 'chatgpt' for ChatGPT Plus/Pro subscriptions.",
		GroupID: "core",
		Example: `  cagent login chatgpt`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			switch provider {
			case "chatgpt":
				return loginChatGPT(cmd)
			default:
				return fmt.Errorf("unsupported provider %q (supported: chatgpt)", provider)
			}
		},
	}

	return cmd
}

func loginChatGPT(cmd *cobra.Command) error {
	fmt.Fprintln(cmd.OutOrStdout(), "Opening browser to authenticate with ChatGPT...")

	token, err := chatgpt.Login(cmd.Context())
	if err != nil {
		return fmt.Errorf("ChatGPT login failed: %w", err)
	}

	if err := chatgpt.SaveToken(token); err != nil {
		return fmt.Errorf("failed to save token: %w", err)
	}

	fmt.Fprintln(cmd.OutOrStdout(), "Successfully authenticated with ChatGPT!")
	fmt.Fprintln(cmd.OutOrStdout(), "You can now use 'chatgpt' as a provider, e.g.: chatgpt/o3")
	return nil
}

func newLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "logout <provider>",
		Short:   "Remove stored authentication for a provider",
		Long:    "Remove stored authentication tokens for a model provider.",
		GroupID: "core",
		Example: `  cagent logout chatgpt`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			provider := args[0]
			switch provider {
			case "chatgpt":
				if err := chatgpt.RemoveToken(); err != nil {
					return fmt.Errorf("failed to remove ChatGPT token: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), "Successfully logged out from ChatGPT.")
				return nil
			default:
				return fmt.Errorf("unsupported provider %q (supported: chatgpt)", provider)
			}
		},
	}

	return cmd
}
