package mflsync

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	source "github.com/tyler180/dynasty-ff-models/analysis"
)

func LoadProjections(path string, defaultSeason int) (source.Projections, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return source.Projections{}, fmt.Errorf("read projections: %w", err)
	}
	var projections source.Projections
	if err := json.Unmarshal(payload, &projections); err != nil {
		return source.Projections{}, fmt.Errorf("decode projections: %w", err)
	}
	if projections.ByPlayerID == nil {
		var values map[string]float64
		if err := json.Unmarshal(payload, &values); err != nil {
			return source.Projections{}, fmt.Errorf("decode projections map: %w", err)
		}
		projections.ByPlayerID = values
	}
	if len(projections.ByPlayerID) == 0 {
		return source.Projections{}, fmt.Errorf("projections.by_player_id cannot be empty")
	}
	if projections.Season == 0 {
		projections.Season = defaultSeason
	}
	if projections.Source == "" {
		projections.Source = filepath.Base(path)
	}
	return projections, nil
}
