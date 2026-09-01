// Package mflfreeagents reads the current defensive free-agent pool through
// the read-only MFL MCP server.
package mflfreeagents

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
)

type Credentials interface {
	Environment(context.Context) (map[string]string, error)
}

type Source struct {
	MCPCommand  string
	Credentials Credentials
}

func (s Source) DefensiveFreeAgents(ctx context.Context, year int, leagueID string) ([]mflsync.DefensiveFreeAgent, error) {
	if strings.TrimSpace(s.MCPCommand) == "" || s.Credentials == nil {
		return nil, fmt.Errorf("MFL MCP command and credentials are required")
	}
	environment, err := s.Credentials.Environment(ctx)
	if err != nil {
		return nil, err
	}
	if environment == nil {
		environment = map[string]string{}
	}
	environment["MFL_YEAR"] = strconv.Itoa(year)
	environment["MFL_LEAGUE_ID"] = leagueID
	client, err := mflsync.ConnectCommandWithEnvironment(ctx, s.MCPCommand, environment)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	var playersPayload, freeAgentsPayload map[string]any
	if err := client.Call(ctx, "get_players", map[string]any{"year": year, "details": true}, &playersPayload); err != nil {
		return nil, err
	}
	if err := client.Call(ctx, "get_free_agents", map[string]any{"year": year, "league_id": leagueID}, &freeAgentsPayload); err != nil {
		return nil, err
	}
	return mflsync.DefensiveFreeAgents(playersPayload, freeAgentsPayload), nil
}
