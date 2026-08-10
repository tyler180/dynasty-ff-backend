package mflsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Caller is the small portion of an MCP client needed by the sync pipeline.
// Tests use an in-memory implementation while the CLI uses CommandClient.
type Caller interface {
	Call(context.Context, string, any, any) error
}

type CommandClient struct {
	session *mcp.ClientSession
}

func ConnectCommand(ctx context.Context, command string, arguments ...string) (*CommandClient, error) {
	if command == "" {
		return nil, errors.New("MCP command is required")
	}
	process := exec.Command(command, arguments...)
	process.Stderr = os.Stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "dynasty-sync", Version: "0.1.0"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: process}, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to MFL MCP server: %w", err)
	}
	return &CommandClient{session: session}, nil
}

func (c *CommandClient) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.Close()
}

func (c *CommandClient) Call(ctx context.Context, tool string, arguments, destination any) error {
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: arguments})
	if err != nil {
		return fmt.Errorf("call %s: %w", tool, err)
	}
	if result.IsError {
		for _, content := range result.Content {
			if text, ok := content.(*mcp.TextContent); ok && text.Text != "" {
				return fmt.Errorf("call %s: %s", tool, text.Text)
			}
		}
		return fmt.Errorf("call %s: MCP tool returned an error", tool)
	}
	if destination == nil {
		return nil
	}
	if result.StructuredContent == nil {
		return fmt.Errorf("call %s: MCP tool returned no structured content", tool)
	}
	payload, err := json.Marshal(result.StructuredContent)
	if err != nil {
		return fmt.Errorf("call %s: encode structured content: %w", tool, err)
	}
	if err := json.Unmarshal(payload, destination); err != nil {
		return fmt.Errorf("call %s: decode structured content: %w", tool, err)
	}
	return nil
}
