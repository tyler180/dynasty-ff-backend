// Package lambdaapp defines the AWS Lambda request boundary for persistence.
package lambdaapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/app/identitysync"
	"github.com/tyler180/dynasty-ff-backend/internal/app/mflingest"
	"github.com/tyler180/dynasty-ff-backend/internal/app/snapshotanalysis"
	"github.com/tyler180/dynasty-ff-backend/internal/domain/league"
	"github.com/tyler180/dynasty-ff-backend/internal/identity"
	"github.com/tyler180/dynasty-ff-backend/internal/identity/player"
	"github.com/tyler180/dynasty-ff-backend/internal/storage/leaguestore"
)

const (
	ActionHealth         = "health"
	ActionPutSnapshot    = "put_snapshot"
	ActionLatestSnapshot = "latest_snapshot"
	ActionSnapshotAt     = "snapshot_at"
	ActionPutPlayer      = "put_player"
	ActionPutAlias       = "put_alias"
	ActionGetPlayer      = "get_player"
	ActionResolvePlayer  = "resolve_player"
	ActionPutIdentities  = "put_identities"
	ActionSyncIdentities = "sync_identities"
	ActionSyncMFL        = "sync_mfl"
	ActionAnalyze        = "analyze"
)

type Request struct {
	Action             string             `json:"action"`
	Snapshot           *league.Snapshot   `json:"snapshot,omitempty"`
	LeagueID           league.ID          `json:"league_id,omitempty"`
	FranchiseID        league.FranchiseID `json:"franchise_id,omitempty"`
	Season             int                `json:"season,omitempty"`
	ObservedAt         string             `json:"observed_at,omitempty"`
	Player             *player.Profile    `json:"player,omitempty"`
	Alias              *identity.Alias    `json:"alias,omitempty"`
	PlayerID           player.ID          `json:"player_id,omitempty"`
	ExternalID         *player.ExternalID `json:"external_id,omitempty"`
	Players            []player.Profile   `json:"players,omitempty"`
	Aliases            []identity.Alias   `json:"aliases,omitempty"`
	IncludeDraft       *bool              `json:"include_draft,omitempty"`
	LiveDraft          bool               `json:"live_draft,omitempty"`
	TimeoutSeconds     int                `json:"timeout_seconds,omitempty"`
	LeagueConfigPath   string             `json:"league_config_path,omitempty"`
	Workers            int                `json:"workers,omitempty"`
	CapReliefTarget    float64            `json:"cap_relief_target,omitempty"`
	ProjectionFallback string             `json:"projection_fallback,omitempty"`
}

type Response struct {
	Action        string                   `json:"action"`
	Status        string                   `json:"status"`
	Snapshot      *league.Snapshot         `json:"snapshot,omitempty"`
	Player        *player.Profile          `json:"player,omitempty"`
	Warnings      []string                 `json:"warnings,omitempty"`
	SyncedAt      *time.Time               `json:"synced_at,omitempty"`
	StoredPlayers int                      `json:"stored_players,omitempty"`
	StoredAliases int                      `json:"stored_aliases,omitempty"`
	IdentitySync  *identitysync.Result     `json:"identity_sync,omitempty"`
	Analysis      *snapshotanalysis.Result `json:"analysis,omitempty"`
}

type Syncer interface {
	Sync(context.Context, mflingest.Request) (mflingest.Result, error)
}

type IdentitySyncer interface {
	Sync(context.Context, identitysync.Request) (identitysync.Result, error)
}

type Analyzer interface {
	Analyze(context.Context, snapshotanalysis.Request) (snapshotanalysis.Result, error)
}

type Handler struct {
	snapshots      leaguestore.Repository
	identities     identity.Repository
	syncer         Syncer
	identitySyncer IdentitySyncer
	analyzer       Analyzer
}

func (h *Handler) WithAnalyzer(analyzer Analyzer) *Handler {
	h.analyzer = analyzer
	return h
}

func (h *Handler) WithSyncer(syncer Syncer) *Handler {
	h.syncer = syncer
	return h
}

func (h *Handler) WithIdentitySyncer(syncer IdentitySyncer) *Handler {
	h.identitySyncer = syncer
	return h
}

func New(snapshots leaguestore.Repository, identities identity.Repository) (*Handler, error) {
	if snapshots == nil {
		return nil, fmt.Errorf("league snapshot repository is required")
	}
	if identities == nil {
		return nil, fmt.Errorf("player identity repository is required")
	}
	return &Handler{snapshots: snapshots, identities: identities}, nil
}

func (h *Handler) Handle(ctx context.Context, request Request) (Response, error) {
	action := strings.TrimSpace(request.Action)
	switch action {
	case ActionHealth:
		return Response{Action: action, Status: "ok"}, nil
	case ActionPutSnapshot:
		if request.Snapshot == nil {
			return Response{}, fmt.Errorf("snapshot is required")
		}
		if err := h.snapshots.PutSnapshot(ctx, *request.Snapshot); err != nil {
			return Response{}, err
		}
		return Response{Action: action, Status: "stored"}, nil
	case ActionLatestSnapshot:
		snapshot, err := h.snapshots.LatestSnapshot(ctx, request.LeagueID, request.FranchiseID, request.Season)
		if err != nil {
			return Response{}, err
		}
		return Response{Action: action, Status: "ok", Snapshot: &snapshot}, nil
	case ActionSnapshotAt:
		observedAt, err := time.Parse(time.RFC3339Nano, request.ObservedAt)
		if err != nil {
			return Response{}, fmt.Errorf("observed_at must be RFC3339: %w", err)
		}
		snapshot, err := h.snapshots.SnapshotAt(ctx, request.LeagueID, request.FranchiseID, request.Season, observedAt)
		if err != nil {
			return Response{}, err
		}
		return Response{Action: action, Status: "ok", Snapshot: &snapshot}, nil
	case ActionPutPlayer:
		if request.Player == nil {
			return Response{}, fmt.Errorf("player is required")
		}
		if err := h.identities.PutPlayer(ctx, *request.Player); err != nil {
			return Response{}, err
		}
		return Response{Action: action, Status: "stored"}, nil
	case ActionPutAlias:
		if request.Alias == nil {
			return Response{}, fmt.Errorf("alias is required")
		}
		if err := h.identities.PutAlias(ctx, *request.Alias); err != nil {
			return Response{}, err
		}
		return Response{Action: action, Status: "stored"}, nil
	case ActionGetPlayer:
		profile, err := h.identities.GetPlayer(ctx, request.PlayerID)
		if err != nil {
			return Response{}, err
		}
		return Response{Action: action, Status: "ok", Player: &profile}, nil
	case ActionResolvePlayer:
		if request.ExternalID == nil {
			return Response{}, fmt.Errorf("external_id is required")
		}
		profile, err := h.identities.ResolvePlayer(ctx, *request.ExternalID)
		if err != nil {
			return Response{}, err
		}
		return Response{Action: action, Status: "ok", Player: &profile}, nil
	case ActionPutIdentities:
		if len(request.Players) == 0 && len(request.Aliases) == 0 {
			return Response{}, fmt.Errorf("players or aliases are required")
		}
		for _, profile := range request.Players {
			if err := h.identities.PutPlayer(ctx, profile); err != nil {
				return Response{}, err
			}
		}
		for _, alias := range request.Aliases {
			if err := h.identities.PutAlias(ctx, alias); err != nil {
				return Response{}, err
			}
		}
		return Response{
			Action: action, Status: "stored",
			StoredPlayers: len(request.Players), StoredAliases: len(request.Aliases),
		}, nil
	case ActionSyncIdentities:
		if h.identitySyncer == nil {
			return Response{}, fmt.Errorf("identity sync is not configured")
		}
		year := request.Season
		if year == 0 {
			year = time.Now().UTC().Year()
		}
		result, err := h.identitySyncer.Sync(ctx, identitysync.Request{Year: year, LeagueID: string(request.LeagueID), Workers: request.Workers})
		if err != nil {
			return Response{}, err
		}
		status := "stored"
		if !result.Complete {
			status = "partial"
		}
		return Response{Action: action, Status: status, IdentitySync: &result}, nil
	case ActionSyncMFL:
		if h.syncer == nil {
			return Response{}, fmt.Errorf("MFL sync is not configured")
		}
		timeout := time.Duration(request.TimeoutSeconds) * time.Second
		if timeout == 0 {
			timeout = 3 * time.Minute
		}
		includeDraft := true
		if request.IncludeDraft != nil {
			includeDraft = *request.IncludeDraft
		}
		result, err := h.syncer.Sync(ctx, mflingest.Request{
			Year: request.Season, LeagueID: string(request.LeagueID), FranchiseID: string(request.FranchiseID),
			LeagueConfigPath: request.LeagueConfigPath, IncludeDraft: includeDraft,
			LiveDraft: request.LiveDraft, Timeout: timeout,
		})
		if err != nil {
			return Response{}, err
		}
		return Response{
			Action: action, Status: "stored", Snapshot: &result.Snapshot,
			Warnings: result.Warnings, SyncedAt: &result.SyncedAt,
		}, nil
	case ActionAnalyze:
		if h.analyzer == nil {
			return Response{}, fmt.Errorf("analysis is not configured")
		}
		result, err := h.analyzer.Analyze(ctx, snapshotanalysis.Request{
			Year: request.Season, LeagueID: request.LeagueID, FranchiseID: request.FranchiseID,
			CapReliefTarget: request.CapReliefTarget, ProjectionFallback: request.ProjectionFallback,
		})
		if err != nil {
			return Response{}, err
		}
		return Response{Action: action, Status: "ok", Analysis: &result}, nil
	default:
		return Response{}, fmt.Errorf("unknown action %q", action)
	}
}
