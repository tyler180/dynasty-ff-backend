package lambdaapp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/events"
)

// Entrypoint accepts both direct Lambda action requests and API Gateway HTTP
// API payload v2 events, preserving EventBridge and operator invocations.
type Entrypoint struct {
	actions ActionHandler
	http    *HTTPHandler
}

func NewEntrypoint(actions ActionHandler) (*Entrypoint, error) {
	httpHandler, err := NewHTTPHandler(actions)
	if err != nil {
		return nil, err
	}
	return &Entrypoint{actions: actions, http: httpHandler}, nil
}

func (e *Entrypoint) Handle(ctx context.Context, payload json.RawMessage) (any, error) {
	var httpEvent events.APIGatewayV2HTTPRequest
	if err := json.Unmarshal(payload, &httpEvent); err == nil && httpEvent.Version == "2.0" && httpEvent.RequestContext.HTTP.Method != "" {
		return e.http.Handle(ctx, httpEvent), nil
	}

	var request Request
	if err := json.Unmarshal(payload, &request); err != nil {
		return nil, fmt.Errorf("decode Lambda request: %w", err)
	}
	return e.actions.Handle(ctx, request)
}
