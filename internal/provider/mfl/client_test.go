package mflsync

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCommandClientCallsStructuredTool(t *testing.T) {
	t.Setenv("DYNASTY_SYNC_MCP_HELPER", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := ConnectCommand(ctx, os.Args[0], "-test.run=TestMCPHelperProcess")
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var result map[string]any
	if err := client.Call(ctx, "fixture", map[string]any{"year": 2026}, &result); err != nil {
		t.Fatal(err)
	}
	if result["league"] != "fixture" || result["year"] != float64(2026) {
		t.Fatalf("result = %#v", result)
	}
	if err := client.Call(ctx, "broken", map[string]any{}, nil); err == nil || !strings.Contains(err.Error(), "fixture failure") {
		t.Fatalf("error = %v, want MCP tool error text", err)
	}
}

func TestMCPHelperProcess(t *testing.T) {
	if os.Getenv("DYNASTY_SYNC_MCP_HELPER") != "1" {
		return
	}
	type input struct {
		Year int `json:"year"`
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "fixture", Version: "test"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "fixture"}, func(_ context.Context, _ *mcp.CallToolRequest, in input) (*mcp.CallToolResult, any, error) {
		structured := map[string]any{"league": "fixture", "year": in.Year}
		return &mcp.CallToolResult{StructuredContent: structured}, nil, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "broken"}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		return nil, nil, errors.New("fixture failure")
	})
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		t.Fatal(err)
	}
}
