package mflsync

import (
	"encoding/json"
	"fmt"
	"os"
)

type LeagueConfig struct {
	LeagueID             string             `json:"league_id"`
	SalaryMultipliers    map[string]float64 `json:"salary_cap_multipliers"`
	RookieSalarySchedule []RookieSalary     `json:"rookie_salary_schedule"`
}

type RookieSalary struct {
	SelectionRange string  `json:"selection_range"`
	Salary         float64 `json:"salary"`
}

func LoadLeagueConfig(path, expectedLeagueID string) (LeagueConfig, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return LeagueConfig{}, fmt.Errorf("read league config: %w", err)
	}
	var config LeagueConfig
	if err := json.Unmarshal(payload, &config); err != nil {
		return LeagueConfig{}, fmt.Errorf("decode league config: %w", err)
	}
	if config.LeagueID == "" {
		return LeagueConfig{}, fmt.Errorf("league config league_id is required")
	}
	if expectedLeagueID != "" && config.LeagueID != expectedLeagueID {
		return LeagueConfig{}, fmt.Errorf("league config is for league %s, not %s", config.LeagueID, expectedLeagueID)
	}
	for status, value := range config.SalaryMultipliers {
		if value < 0 || value > 1 {
			return LeagueConfig{}, fmt.Errorf("league config salary multiplier %s must be between 0 and 1", status)
		}
	}
	for index, entry := range config.RookieSalarySchedule {
		if entry.SelectionRange == "" || entry.Salary < 0 {
			return LeagueConfig{}, fmt.Errorf("league config rookie_salary_schedule[%d] is invalid", index)
		}
	}
	return config, nil
}

func ApplyLeagueConfig(base *BaseDocument, config LeagueConfig) {
	if base.Raw == nil {
		base.Raw = make(map[string]any)
	}
	if base.SalaryMultipliers == nil {
		base.SalaryMultipliers = make(map[string]float64)
	}
	for status, value := range config.SalaryMultipliers {
		base.SalaryMultipliers[status] = value
	}
	if len(config.SalaryMultipliers) > 0 {
		base.HasSalaryMultipliers = true
		league, ok := object(base.Raw["league"])
		if !ok {
			league = make(map[string]any)
			base.Raw["league"] = league
		}
		league["salary_cap_multipliers"] = config.SalaryMultipliers
	}
	if len(config.RookieSalarySchedule) > 0 {
		base.HasRookieSalarySchedule = true
		draft, ok := object(base.Raw["draft"])
		if !ok {
			draft = make(map[string]any)
			base.Raw["draft"] = draft
		}
		entries := make([]any, 0, len(config.RookieSalarySchedule))
		for _, entry := range config.RookieSalarySchedule {
			entries = append(entries, map[string]any{
				"selection_range": entry.SelectionRange,
				"salary":          entry.Salary,
			})
		}
		draft["rookie_salary_schedule"] = entries
	}
}
