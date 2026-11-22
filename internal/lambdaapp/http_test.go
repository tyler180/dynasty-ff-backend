package lambdaapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

type recordingActionHandler struct {
	request Request
	result  Response
	err     error
}

func (h *recordingActionHandler) Handle(_ context.Context, request Request) (Response, error) {
	h.request = request
	return h.result, h.err
}

func TestHTTPHandlerMapsAnalyzeWithoutAcceptingAnAction(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionAnalyze, Status: "ok"}}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("POST", "/v1/analyze")
	event.Headers = map[string]string{"content-type": "application/json"}
	event.Body = `{"season":2026,"league_id":"79286","franchise_id":"0005","cap_relief_target":10,"projection_fallback":"auto"}`

	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 200 {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	if actions.request.Action != ActionAnalyze || actions.request.CapReliefTarget != 10 {
		t.Fatalf("request = %+v", actions.request)
	}

	event.Body = `{"action":"sync_mfl","season":2026,"league_id":"79286","franchise_id":"0005"}`
	response = handler.Handle(context.Background(), event)
	if response.StatusCode != 400 {
		t.Fatalf("injected action status = %d, want 400", response.StatusCode)
	}
	if actions.request.Action != ActionAnalyze {
		t.Fatalf("mutating action reached handler: %+v", actions.request)
	}
}

func TestHTTPHandlerMapsSnapshotSyncModes(t *testing.T) {
	tests := []struct {
		name          string
		body          string
		wantStatus    int
		wantLiveDraft bool
	}{
		{name: "mode omitted is a team refresh", body: `{"season":2026,"league_id":"79286","franchise_id":"0005"}`, wantStatus: 202},
		{name: "team", body: `{"season":2026,"league_id":"79286","franchise_id":"0005","mode":"team"}`, wantStatus: 202},
		{name: "draft", body: `{"season":2026,"league_id":"79286","franchise_id":"0005","mode":"draft"}`, wantStatus: 202, wantLiveDraft: true},
		{name: "unknown mode", body: `{"season":2026,"league_id":"79286","franchise_id":"0005","mode":"fast"}`, wantStatus: 400},
		{name: "injected action", body: `{"action":"put_snapshot","season":2026,"league_id":"79286","franchise_id":"0005"}`, wantStatus: 400},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &recordingActionHandler{result: Response{Action: ActionStartMFLSync, Status: "accepted"}}
			handler, err := NewHTTPHandler(actions)
			if err != nil {
				t.Fatal(err)
			}
			event := httpEvent("POST", "/v1/snapshots/sync")
			event.Headers = map[string]string{"content-type": "application/json"}
			event.Body = test.body

			response := handler.Handle(context.Background(), event)
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", response.StatusCode, test.wantStatus, response.Body)
			}
			if test.wantStatus != 202 {
				if actions.request.Action != "" {
					t.Fatalf("rejected request reached handler: %+v", actions.request)
				}
				return
			}
			request := actions.request
			if request.Action != ActionStartMFLSync || request.IncludeDraft == nil || !*request.IncludeDraft || request.LiveDraft != test.wantLiveDraft {
				t.Fatalf("sync request = %+v", request)
			}
		})
	}
}

func TestHTTPHandlerMapsSnapshotQueries(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionLatestSnapshot, Status: "ok"}}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("GET", "/v1/snapshots/latest")
	event.QueryStringParameters = map[string]string{
		"season": "2026", "league_id": "79286", "franchise_id": "0005",
	}

	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 200 || actions.request.Action != ActionLatestSnapshot {
		t.Fatalf("response/request = %+v / %+v", response, actions.request)
	}
	if actions.request.Season != 2026 || actions.request.LeagueID != "79286" || actions.request.FranchiseID != "0005" {
		t.Fatalf("request = %+v", actions.request)
	}
}

func TestHTTPHandlerMapsDefensiveSnapQuery(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionGetSnapCounts, Status: "ok"}}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("GET", "/v1/players/snaps")
	event.QueryStringParameters = map[string]string{
		"player_ids": "player-1,player-2", "seasons": "2024,2025", "position_groups": "lb,db",
	}
	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 200 || actions.request.Action != ActionGetSnapCounts {
		t.Fatalf("response/request = %+v / %+v", response, actions.request)
	}
	if len(actions.request.PlayerIDs) != 2 || len(actions.request.Seasons) != 2 || actions.request.PositionGroups[0] != "LB" {
		t.Fatalf("request = %+v", actions.request)
	}
}

func TestHTTPHandlerMapsPlayerStatsQuery(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionGetPlayerStats, Status: "ok"}}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("GET", "/v1/players/stats")
	event.QueryStringParameters = map[string]string{"player_ids": "player-1,player-2", "seasons": "2024,2025"}
	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 200 || actions.request.Action != ActionGetPlayerStats || len(actions.request.PlayerIDs) != 2 || len(actions.request.Seasons) != 2 {
		t.Fatalf("response/request = %+v / %+v", response, actions.request)
	}
}

func TestHTTPHandlerMapsDepthChartQuery(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionGetDepthCharts, Status: "ok"}}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("GET", "/v1/players/depth-charts")
	event.QueryStringParameters = map[string]string{"player_ids": "player-1,player-2", "seasons": "2025,2026"}
	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 200 || actions.request.Action != ActionGetDepthCharts || len(actions.request.PlayerIDs) != 2 || len(actions.request.Seasons) != 2 {
		t.Fatalf("response/request = %+v / %+v", response, actions.request)
	}
}

func TestHTTPHandlerMapsDefensiveFreeAgentTrendQuery(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionTopDefensiveFreeAgentTrends, Status: "ok"}}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("GET", "/v1/free-agents/defensive-trends")
	event.QueryStringParameters = map[string]string{
		"season": "2026", "league_id": "79286", "seasons": "2023,2024,2025", "limit": "10",
	}
	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 200 || actions.request.Action != ActionTopDefensiveFreeAgentTrends {
		t.Fatalf("response/request = %+v / %+v", response, actions.request)
	}
	if actions.request.Season != 2026 || actions.request.LeagueID != "79286" || actions.request.Limit != 10 || len(actions.request.Seasons) != 3 {
		t.Fatalf("request = %+v", actions.request)
	}
}

func TestHTTPHandlerRequiresSnapshotAtTimestamp(t *testing.T) {
	actions := &recordingActionHandler{}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("GET", "/v1/snapshots/at")
	event.QueryStringParameters = map[string]string{
		"season": "2026", "league_id": "79286", "franchise_id": "0005",
	}

	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", response.StatusCode)
	}
	if actions.request.Action != "" {
		t.Fatalf("invalid request reached action handler: %+v", actions.request)
	}
}

func TestHTTPHandlerRejectsMalformedSnapshotAtTimestamp(t *testing.T) {
	actions := &recordingActionHandler{}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("GET", "/v1/snapshots/at")
	event.QueryStringParameters = map[string]string{
		"season": "2026", "league_id": "79286", "franchise_id": "0005", "observed_at": "yesterday",
	}

	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 400 || actions.request.Action != "" {
		t.Fatalf("response/request = %+v / %+v", response, actions.request)
	}
}

func TestHTTPHandlerSupportsBase64BodyAndHidesInternalErrors(t *testing.T) {
	actions := &recordingActionHandler{err: context.DeadlineExceeded}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("POST", "/v1/analyze")
	event.Body = base64.StdEncoding.EncodeToString([]byte(`{"season":2026,"league_id":"79286","franchise_id":"0005"}`))
	event.IsBase64Encoded = true

	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 500 {
		t.Fatalf("status = %d, want 500", response.StatusCode)
	}
	if json.Valid([]byte(response.Body)) == false || response.Body == "" {
		t.Fatalf("invalid error response %q", response.Body)
	}
	if response.Body == context.DeadlineExceeded.Error() {
		t.Fatalf("internal error leaked: %q", response.Body)
	}
}

func TestHTTPHandlerRejectsUnmappedRoutes(t *testing.T) {
	actions := &recordingActionHandler{}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	response := handler.Handle(context.Background(), httpEvent("POST", "/v1/actions"))
	if response.StatusCode != 404 || actions.request.Action != "" {
		t.Fatalf("response/request = %+v / %+v", response, actions.request)
	}
}

func TestEntrypointPreservesDirectLambdaRequests(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionHealth, Status: "ok"}}
	entrypoint, err := NewEntrypoint(actions)
	if err != nil {
		t.Fatal(err)
	}
	result, err := entrypoint.Handle(context.Background(), json.RawMessage(`{"action":"health"}`))
	if err != nil {
		t.Fatal(err)
	}
	response, ok := result.(Response)
	if !ok || response.Status != "ok" || actions.request.Action != ActionHealth {
		t.Fatalf("result/request = %#v / %+v", result, actions.request)
	}
}

func TestEntrypointDispatchesHTTPV2Events(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionHealth, Status: "ok"}}
	entrypoint, err := NewEntrypoint(actions)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(httpEvent("GET", "/health"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := entrypoint.Handle(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	response, ok := result.(events.APIGatewayV2HTTPResponse)
	if !ok || response.StatusCode != 200 || actions.request.Action != ActionHealth {
		t.Fatalf("result/request = %#v / %+v", result, actions.request)
	}
}

func httpEvent(method, path string) events.APIGatewayV2HTTPRequest {
	return events.APIGatewayV2HTTPRequest{
		Version: "2.0", RawPath: path,
		RequestContext: events.APIGatewayV2HTTPRequestContext{
			HTTP: events.APIGatewayV2HTTPRequestContextHTTPDescription{Method: method, Path: path},
		},
	}
}

func TestHTTPHandlerParsesDefensiveFreeAgentTrendMaxAge(t *testing.T) {
	tests := []struct {
		name       string
		maxAge     string
		wantStatus int
		wantMaxAge int
	}{
		{"omitted means no filter", "", 200, 0},
		{"valid", "28", 200, 28},
		{"zero is rejected", "0", 400, 0},
		{"too large", "61", 400, 0},
		{"not a number", "young", 400, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &recordingActionHandler{result: Response{Action: ActionTopDefensiveFreeAgentTrends, Status: "ok"}}
			handler, err := NewHTTPHandler(actions)
			if err != nil {
				t.Fatal(err)
			}
			event := httpEvent("GET", "/v1/free-agents/defensive-trends")
			event.QueryStringParameters = map[string]string{"season": "2026", "league_id": "79286"}
			if test.maxAge != "" {
				event.QueryStringParameters["max_age"] = test.maxAge
			}
			response := handler.Handle(context.Background(), event)
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", response.StatusCode, test.wantStatus, response.Body)
			}
			if test.wantStatus == 200 && actions.request.MaxAge != test.wantMaxAge {
				t.Fatalf("max age = %d, want %d", actions.request.MaxAge, test.wantMaxAge)
			}
		})
	}
}

func TestHTTPHandlerParsesDefensiveFreeAgentTrendRecencyWeeks(t *testing.T) {
	weeks := func(value int) *int { return &value }
	tests := []struct {
		name       string
		value      string
		wantStatus int
		want       *int
	}{
		{"omitted uses the service default", "", 200, nil},
		{"valid", "4", 200, weeks(4)},
		{"zero turns the filter off", "0", 200, weeks(0)},
		{"negative", "-1", 400, nil},
		{"too large", "19", 400, nil},
		{"not a number", "recent", 400, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &recordingActionHandler{result: Response{Action: ActionTopDefensiveFreeAgentTrends, Status: "ok"}}
			handler, err := NewHTTPHandler(actions)
			if err != nil {
				t.Fatal(err)
			}
			event := httpEvent("GET", "/v1/free-agents/defensive-trends")
			event.QueryStringParameters = map[string]string{"season": "2026", "league_id": "79286"}
			if test.value != "" {
				event.QueryStringParameters["recency_weeks"] = test.value
			}
			response := handler.Handle(context.Background(), event)
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", response.StatusCode, test.wantStatus, response.Body)
			}
			if test.wantStatus == 200 && !reflect.DeepEqual(actions.request.RecencyWeeks, test.want) {
				t.Fatalf("recency weeks = %v, want %v", actions.request.RecencyWeeks, test.want)
			}
		})
	}
}

func TestHTTPHandlerParsesDefensiveFreeAgentTrendPositionGroups(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		wantStatus int
		want       []string
	}{
		{"omitted shows every group", "", 200, nil},
		{"one group", "LB", 200, []string{"LB"}},
		{"several, lower case and spaced", "dl, db", 200, []string{"DL", "DB"}},
		{"empty entries are skipped", "LB,,", 200, []string{"LB"}},
		{"offensive position", "QB", 400, nil},
		{"specific position", "CB", 400, nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			actions := &recordingActionHandler{result: Response{Action: ActionTopDefensiveFreeAgentTrends, Status: "ok"}}
			handler, err := NewHTTPHandler(actions)
			if err != nil {
				t.Fatal(err)
			}
			event := httpEvent("GET", "/v1/free-agents/defensive-trends")
			event.QueryStringParameters = map[string]string{"season": "2026", "league_id": "79286"}
			if test.value != "" {
				event.QueryStringParameters["position_groups"] = test.value
			}
			response := handler.Handle(context.Background(), event)
			if response.StatusCode != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", response.StatusCode, test.wantStatus, response.Body)
			}
			if test.wantStatus == 200 && !reflect.DeepEqual(actions.request.PositionGroups, test.want) {
				t.Fatalf("position groups = %v, want %v", actions.request.PositionGroups, test.want)
			}
		})
	}
}
