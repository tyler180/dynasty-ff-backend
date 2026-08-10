package source

import (
	"fmt"
	"strings"
)

func FormatText(analysis Analysis) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s — %s\n", analysis.Team, analysis.League)
	fmt.Fprintf(&out, "Snapshot: %s\n\n", analysis.SnapshotDate)
	fmt.Fprintf(&out, "CAP AND ROSTER\n")
	fmt.Fprintf(&out, "  Cap: $%.2f / $%.2f ($%.2f available)\n", analysis.Cap.Used, analysis.Cap.Limit, analysis.Cap.Space)
	fmt.Fprintf(&out, "  Active: %d / %d (%d open)\n", analysis.Roster.Active.Used, analysis.Roster.Active.Limit, analysis.Roster.Active.Open)
	fmt.Fprintf(&out, "  IR: %d / %d (%d open)\n", analysis.Roster.InjuredReserve.Used, analysis.Roster.InjuredReserve.Limit, analysis.Roster.InjuredReserve.Open)
	fmt.Fprintf(&out, "  Taxi: %d / %d (%d open)\n\n", analysis.Roster.Taxi.Used, analysis.Roster.Taxi.Limit, analysis.Roster.Taxi.Open)

	fmt.Fprintf(&out, "TAXI COMPLIANCE\n")
	if len(analysis.TaxiCompliance.MustLeaveTaxi) == 0 {
		fmt.Fprintf(&out, "  No known age-based taxi violations.\n")
	} else {
		fmt.Fprintf(&out, "  %d player(s) must leave taxi before compliance: %s.\n",
			len(analysis.TaxiCompliance.MustLeaveTaxi), playerList(analysis.TaxiCompliance.MustLeaveTaxi))
		fmt.Fprintf(&out, "  Moving them opens %d taxi slot(s), enough for the first %d pick(s).\n",
			analysis.TaxiCompliance.SlotsOpenedAfterMove,
			min(analysis.TaxiCompliance.SlotsOpenedAfterMove, len(analysis.Draft.Picks)))
	}
	for _, scenario := range analysis.ComplianceScenarios {
		status := "legal"
		if !scenario.RosterLegal {
			status = fmt.Sprintf("needs $%.2f additional cap relief", scenario.AdditionalCapRelief)
		}
		fmt.Fprintf(&out, "  - %s: promote [%s]; remove/trade [%s]; stash picks [%s] on taxi -> active %d, taxi %d, cap $%.2f (%s)\n",
			scenario.Name, playerList(scenario.Promote), playerList(scenario.RemoveOrTrade), strings.Join(scenario.FirstPicksToTaxi, ", "),
			scenario.ResultingActive, scenario.ResultingTaxi, scenario.ResultingCapHit, status)
	}
	fmt.Fprintln(&out)

	fmt.Fprintf(&out, "2026 DRAFT\n")
	fmt.Fprintf(&out, "  Status: %s", strings.ReplaceAll(analysis.Draft.Status, "_", " "))
	if analysis.Draft.AvailabilityWindow.Start != "" {
		fmt.Fprintf(&out, " (availability poll %s through %s)", analysis.Draft.AvailabilityWindow.Start, analysis.Draft.AvailabilityWindow.End)
	}
	fmt.Fprintln(&out)
	fmt.Fprintf(&out, "  %d picks; $%.2f total salary if every pick were used on an active player.\n", analysis.Draft.PickCount, analysis.Draft.TotalSalaryIfActive)
	for _, pick := range analysis.Draft.Picks {
		active := "fits active now"
		if !pick.FitsActiveNow {
			if pick.ActiveCapShortfall > 0 {
				active = fmt.Sprintf("active needs $%.2f cap relief", pick.ActiveCapShortfall)
			} else {
				active = "active needs a roster slot"
			}
		}
		taxi := "fits taxi now"
		if !pick.FitsTaxiNow {
			taxi = "taxi needs a slot"
		}
		fmt.Fprintf(&out, "  - %s (#%d), $%.2f: %s; %s\n", pick.Pick, pick.Overall, pick.Salary, active, taxi)
	}
	fmt.Fprintln(&out)

	fmt.Fprintf(&out, "%s WEIGHTED CAP EFFICIENCY (CONTEXT ONLY)\n", formatSeasons(analysis.HistoricalEfficiency.Seasons))
	fmt.Fprintf(&out, "  Method: %s\n", analysis.HistoricalEfficiency.Method)
	fmt.Fprintf(&out, "  Most efficient: %s\n", efficiencyList(analysis.HistoricalEfficiency.MostEfficient))
	fmt.Fprintf(&out, "  Least efficient: %s\n", efficiencyList(analysis.HistoricalEfficiency.LeastEfficient))
	fmt.Fprintf(&out, "  Caution: %s\n\n", analysis.HistoricalEfficiency.Caution)

	fmt.Fprintf(&out, "CAP-CUT EVALUATION\n")
	if !analysis.DropEvaluation.Available {
		fmt.Fprintf(&out, "  Unavailable: pass -projections with player-ID values, or opt into -projection-fallback historical.\n\n")
	} else {
		fmt.Fprintf(&out, "  Production input: %s\n", analysis.DropEvaluation.ProjectionSource)
		if analysis.DropEvaluation.ReplacementSource != "" {
			fmt.Fprintf(&out, "  Replacement input: %s\n", analysis.DropEvaluation.ReplacementSource)
		}
		if analysis.DropEvaluation.BestForTarget != nil {
			best := analysis.DropEvaluation.BestForTarget
			fmt.Fprintf(&out, "  Best DROP candidate for at least $%.2f relief: %s (%s, age %d) — saves $%.2f, %.2f %s, drop score %.3f\n",
				analysis.DropEvaluation.CapReliefTarget, best.Name, best.Position, best.Age, best.SalaryCapRelief, best.ProductionValue, analysis.DropEvaluation.ProductionMetric, best.DropScore)
		} else {
			fmt.Fprintf(&out, "  No player classified as a DROP candidate provides the $%.2f target relief; review trade-first options instead.\n", analysis.DropEvaluation.CapReliefTarget)
		}
		writeCandidateSection(&out, "DROP CANDIDATES", analysis.DropEvaluation.DropCandidates, analysis.DropEvaluation.ProductionMetric)
		writeCandidateSection(&out, "TRADE FIRST — DO NOT DROP", analysis.DropEvaluation.TradeFirst, analysis.DropEvaluation.ProductionMetric)
		writeCandidateSection(&out, "HOLD / DEVELOP", analysis.DropEvaluation.HoldDevelop, analysis.DropEvaluation.ProductionMetric)
		fmt.Fprintf(&out, "  Caution: %s\n\n", analysis.DropEvaluation.Caution)
	}

	fmt.Fprintf(&out, "LIMITATIONS\n")
	for _, warning := range analysis.Warnings {
		fmt.Fprintf(&out, "  - %s\n", warning)
	}
	return out.String()
}

func writeCandidateSection(out *strings.Builder, label string, candidates []DropCandidate, metric string) {
	if len(candidates) == 0 {
		return
	}
	limit := min(10, len(candidates))
	fmt.Fprintf(out, "  %s (%d):\n", label, len(candidates))
	for _, candidate := range candidates[:limit] {
		if candidate.Disposition == "cap_efficiency_only" {
			fmt.Fprintf(out, "    - %s (%s, age %d): $%.2f relief / %.2f %s / %.3f score\n",
				candidate.Name, candidate.Position, candidate.Age, candidate.SalaryCapRelief, candidate.ProductionValue, metric, candidate.DropScore)
			continue
		}
		switch candidate.Disposition {
		case "drop_candidate":
			fmt.Fprintf(out, "    - %s (%s, age %d): $%.2f relief; %.2f PPG vs %.2f replacement (VORP %+.2f); %.3f drop score\n",
				candidate.Name, candidate.Position, candidate.Age, candidate.SalaryCapRelief, candidate.ProductionValue,
				candidate.ReplacementPointsPerGame, candidate.ValueOverReplacement, candidate.DropScore)
		case "trade_first":
			fmt.Fprintf(out, "    - %s (%s, age %d): $%.2f relief; %.2f PPG vs %.2f replacement (VORP %+.2f); trade before cutting\n",
				candidate.Name, candidate.Position, candidate.Age, candidate.SalaryCapRelief, candidate.ProductionValue,
				candidate.ReplacementPointsPerGame, candidate.ValueOverReplacement)
		case "hold_develop":
			fmt.Fprintf(out, "    - %s (%s, age %d): %d season(s), %.2f development factor; hold unless an explicit dynasty valuation says otherwise\n",
				candidate.Name, candidate.Position, candidate.Age, candidate.CareerSeasons, candidate.DevelopmentFactor)
		}
	}
}

func playerList[T interface{ ~[]PlayerSummary }](players T) string {
	if len(players) == 0 {
		return "none"
	}
	labels := make([]string, 0, len(players))
	for _, player := range players {
		labels = append(labels, fmt.Sprintf("%s ($%.0f)", player.Name, player.Salary))
	}
	return strings.Join(labels, ", ")
}

func efficiencyList(players []EfficiencyPlayer) string {
	if len(players) == 0 {
		return "unavailable"
	}
	labels := make([]string, 0, len(players))
	for _, player := range players {
		labels = append(labels, fmt.Sprintf("%s %.2f PPG/$", player.Name, player.PointsPerGamePerSalary))
	}
	return strings.Join(labels, "; ")
}
