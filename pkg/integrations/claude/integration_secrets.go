package claude

import (
	"fmt"
	"strings"

	"github.com/superplanehq/superplane/pkg/core"
)

const (
	integrationSecretAnthropicAPIKey = "ANTHROPIC_API_KEY"
	// Claude Code reads a subscription token from this variable. The same token
	// in ANTHROPIC_API_KEY is rejected as an invalid key.
	integrationSecretClaudeCodeOAuthToken = "CLAUDE_CODE_OAUTH_TOKEN"
	claudeOAuthSecretUsage                = `A Claude Code OAuth token is available in the CLAUDE_CODE_OAUTH_TOKEN environment variable.
Do not print the token.`
	claudeSecretUsage = `An Anthropic API key is available in the ANTHROPIC_API_KEY environment variable.
Do not print the key.`
)

func (i *Claude) ResolveSecrets(ctx core.IntegrationSecretContext) (core.IntegrationSecrets, error) {
	apiKey, err := ctx.Integration.GetConfig("apiKey")
	if err != nil {
		return core.IntegrationSecrets{}, fmt.Errorf("failed to get API key: %w", err)
	}

	key := strings.TrimSpace(string(apiKey))
	if key == "" {
		return core.IntegrationSecrets{}, fmt.Errorf("apiKey is required")
	}

	if strings.HasPrefix(key, oauthTokenPrefix) {
		return core.IntegrationSecrets{
			Values: map[string][]byte{
				integrationSecretClaudeCodeOAuthToken: []byte(key),
			},
			Usage: claudeOAuthSecretUsage,
		}, nil
	}

	return core.IntegrationSecrets{
		Values: map[string][]byte{
			integrationSecretAnthropicAPIKey: []byte(key),
		},
		Usage: claudeSecretUsage,
	}, nil
}
