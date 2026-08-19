package lambdaapp

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
)

const maxHTTPBodyBytes = 1 << 20

type ActionHandler interface {
	Handle(context.Context, Request) (Response, error)
}

// HTTPHandler exposes a deliberately small HTTP surface. API Gateway owns
// authentication; this adapter owns route-to-action authorization so a caller
// cannot smuggle an arbitrary Lambda action through an authenticated route.
type HTTPHandler struct {
	actions ActionHandler
}

func NewHTTPHandler(actions ActionHandler) (*HTTPHandler, error) {
	if actions == nil {
		return nil, fmt.Errorf("action handler is required")
	}
	return &HTTPHandler{actions: actions}, nil
}

func (h *HTTPHandler) Handle(ctx context.Context, event events.APIGatewayV2HTTPRequest) events.APIGatewayV2HTTPResponse {
	method := strings.ToUpper(strings.TrimSpace(event.RequestContext.HTTP.Method))
	path := strings.TrimSuffix(event.RawPath, "/")
	if path == "" {
		path = "/"
	}

	var (
		request Request
		err     error
	)
	switch method + " " + path {
	case http.MethodGet + " /health":
		request = Request{Action: ActionHealth}
	case http.MethodPost + " /v1/analyze":
		request, err = decodeAnalyzeRequest(event)
	case http.MethodPost + " /v1/snapshots/sync":
		request, err = decodeSnapshotSyncRequest(event)
	case http.MethodGet + " /v1/snapshots/latest":
		request, err = decodeSnapshotRequest(ActionLatestSnapshot, event.QueryStringParameters)
	case http.MethodGet + " /v1/snapshots/at":
		request, err = decodeSnapshotRequest(ActionSnapshotAt, event.QueryStringParameters)
	default:
		return jsonHTTPResponse(http.StatusNotFound, errorBody{Error: "not_found", Message: "route not found"})
	}
	if err != nil {
		return jsonHTTPResponse(http.StatusBadRequest, errorBody{Error: "invalid_request", Message: err.Error()})
	}

	response, err := h.actions.Handle(ctx, request)
	if err != nil {
		log.Printf("HTTP action %s failed: %v", request.Action, err)
		return jsonHTTPResponse(http.StatusInternalServerError, errorBody{
			Error: "action_failed", Message: "the requested operation could not be completed",
		})
	}
	return jsonHTTPResponse(http.StatusOK, response)
}

type analyzeHTTPBody struct {
	Season             int                `json:"season"`
	LeagueID           league.ID          `json:"league_id"`
	FranchiseID        league.FranchiseID `json:"franchise_id"`
	CapReliefTarget    float64            `json:"cap_relief_target,omitempty"`
	ProjectionFallback string             `json:"projection_fallback,omitempty"`
}

type snapshotSyncHTTPBody struct {
	Season      int                `json:"season"`
	LeagueID    league.ID          `json:"league_id"`
	FranchiseID league.FranchiseID `json:"franchise_id"`
}

func decodeAnalyzeRequest(event events.APIGatewayV2HTTPRequest) (Request, error) {
	var input analyzeHTTPBody
	if err := decodeJSONBody(event, &input); err != nil {
		return Request{}, err
	}
	if err := validateLeagueCoordinates(input.Season, input.LeagueID, input.FranchiseID); err != nil {
		return Request{}, err
	}
	if input.CapReliefTarget < 0 {
		return Request{}, fmt.Errorf("cap_relief_target cannot be negative")
	}
	if input.ProjectionFallback != "" && input.ProjectionFallback != "auto" && input.ProjectionFallback != "historical" && input.ProjectionFallback != "none" {
		return Request{}, fmt.Errorf("projection_fallback must be auto, historical, or none")
	}
	return Request{
		Action: ActionAnalyze, Season: input.Season, LeagueID: input.LeagueID,
		FranchiseID: input.FranchiseID, CapReliefTarget: input.CapReliefTarget,
		ProjectionFallback: input.ProjectionFallback,
	}, nil
}

func decodeSnapshotSyncRequest(event events.APIGatewayV2HTTPRequest) (Request, error) {
	var input snapshotSyncHTTPBody
	if err := decodeJSONBody(event, &input); err != nil {
		return Request{}, err
	}
	if err := validateLeagueCoordinates(input.Season, input.LeagueID, input.FranchiseID); err != nil {
		return Request{}, err
	}
	includeDraft := true
	return Request{
		Action: ActionSyncMFL, Season: input.Season, LeagueID: input.LeagueID,
		FranchiseID: input.FranchiseID, IncludeDraft: &includeDraft, LiveDraft: true,
		SkipFantasyPros: true, TimeoutSeconds: 25,
	}, nil
}

func decodeJSONBody(event events.APIGatewayV2HTTPRequest, destination any) error {
	contentType := strings.TrimSpace(event.Headers["content-type"])
	if contentType == "" {
		contentType = strings.TrimSpace(event.Headers["Content-Type"])
	}
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || mediaType != "application/json" {
			return fmt.Errorf("content-type must be application/json")
		}
	}

	body := []byte(event.Body)
	if event.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(event.Body)
		if err != nil {
			return fmt.Errorf("body is not valid base64")
		}
		body = decoded
	}
	if len(body) == 0 {
		return fmt.Errorf("JSON body is required")
	}
	if len(body) > maxHTTPBodyBytes {
		return fmt.Errorf("request body exceeds 1 MiB")
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func decodeSnapshotRequest(action string, query map[string]string) (Request, error) {
	season, err := strconv.Atoi(strings.TrimSpace(query["season"]))
	if err != nil {
		return Request{}, fmt.Errorf("season must be an integer")
	}
	request := Request{
		Action: action, Season: season, LeagueID: league.ID(strings.TrimSpace(query["league_id"])),
		FranchiseID: league.FranchiseID(strings.TrimSpace(query["franchise_id"])),
	}
	if err := validateLeagueCoordinates(request.Season, request.LeagueID, request.FranchiseID); err != nil {
		return Request{}, err
	}
	if action == ActionSnapshotAt {
		request.ObservedAt = strings.TrimSpace(query["observed_at"])
		if request.ObservedAt == "" {
			return Request{}, fmt.Errorf("observed_at is required")
		}
		if _, err := time.Parse(time.RFC3339Nano, request.ObservedAt); err != nil {
			return Request{}, fmt.Errorf("observed_at must be RFC3339")
		}
	}
	return request, nil
}

func validateLeagueCoordinates(season int, leagueID league.ID, franchiseID league.FranchiseID) error {
	if season < 2000 || season > 2100 {
		return fmt.Errorf("season must be between 2000 and 2100")
	}
	if strings.TrimSpace(string(leagueID)) == "" {
		return fmt.Errorf("league_id is required")
	}
	if strings.TrimSpace(string(franchiseID)) == "" {
		return fmt.Errorf("franchise_id is required")
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("JSON body must contain exactly one object")
	} else if err != io.EOF {
		return fmt.Errorf("invalid trailing JSON: %w", err)
	}
	return nil
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func jsonHTTPResponse(status int, body any) events.APIGatewayV2HTTPResponse {
	payload, err := json.Marshal(body)
	if err != nil {
		status = http.StatusInternalServerError
		payload = []byte(`{"error":"internal_error","message":"response encoding failed"}`)
	}
	return events.APIGatewayV2HTTPResponse{
		StatusCode: status,
		Headers: map[string]string{
			"content-type":  "application/json; charset=utf-8",
			"cache-control": "no-store",
		},
		Body: string(payload),
	}
}
