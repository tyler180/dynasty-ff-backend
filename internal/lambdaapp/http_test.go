package lambdaapp

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

func TestHTTPHandlerMapsSnapshotSyncToFastLiveDraftRefresh(t *testing.T) {
	actions := &recordingActionHandler{result: Response{Action: ActionStartMFLSync, Status: "accepted"}}
	handler, err := NewHTTPHandler(actions)
	if err != nil {
		t.Fatal(err)
	}
	event := httpEvent("POST", "/v1/snapshots/sync")
	event.Headers = map[string]string{"content-type": "application/json"}
	event.Body = `{"season":2026,"league_id":"79286","franchise_id":"0005"}`

	response := handler.Handle(context.Background(), event)
	if response.StatusCode != 202 {
		t.Fatalf("status = %d, body = %s", response.StatusCode, response.Body)
	}
	request := actions.request
	if request.Action != ActionStartMFLSync || request.IncludeDraft == nil || !*request.IncludeDraft {
		t.Fatalf("sync request = %+v", request)
	}

	event.Body = `{"action":"put_snapshot","season":2026,"league_id":"79286","franchise_id":"0005"}`
	response = handler.Handle(context.Background(), event)
	if response.StatusCode != 400 {
		t.Fatalf("injected action status = %d, want 400", response.StatusCode)
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
