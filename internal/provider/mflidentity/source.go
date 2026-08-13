// Package mflidentity reads the current MFL player catalog for identity sync.
package mflidentity

import (
	"context"
	"fmt"
	"strconv"

	mflsync "github.com/tyler180/dynasty-ff-backend/internal/provider/mfl"
)

type Credentials interface {
	Environment(context.Context) (map[string]string, error)
}

type Source struct {
	MCPCommand  string
	Credentials Credentials
}

func (s Source) PlayerIDs(ctx context.Context, year int, leagueID string) (map[string]struct{}, error) {
	if s.MCPCommand == "" || s.Credentials == nil {
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
	arguments := map[string]any{"year": year, "league_id": leagueID}
	var rostersPayload, freeAgentsPayload map[string]any
	if err := client.Call(ctx, "get_rosters", arguments, &rostersPayload); err != nil {
		return nil, err
	}
	if err := client.Call(ctx, "get_free_agents", arguments, &freeAgentsPayload); err != nil {
		return nil, err
	}
	ids := mflsync.LeaguePlayerIDs(rostersPayload, freeAgentsPayload)
	if len(ids) == 0 {
		return nil, fmt.Errorf("MFL rosters and free agents returned no player records")
	}
	result := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		result[id] = struct{}{}
	}
	return result, nil
}
