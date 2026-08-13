package mflsync

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tyler180/dynasty-ff-backend/internal/analysis/source"
)

type catalogPlayer struct {
	ID         string
	Name       string
	Position   string
	NFLTeam    string
	RookieYear int
	Birthdate  int64
}

func parseCatalog(payload map[string]any) map[string]catalogPlayer {
	result := make(map[string]catalogPlayer)
	root := envelope(payload, "players")
	for _, item := range values(root["player"]) {
		player, ok := object(item)
		if !ok {
			continue
		}
		id := textField(player, "id")
		if id == "" {
			continue
		}
		birthdate, _ := strconv.ParseInt(textField(player, "birthdate"), 10, 64)
		result[id] = catalogPlayer{
			ID:         id,
			Name:       displayName(textField(player, "name")),
			Position:   strings.ToUpper(textField(player, "position", "pos")),
			NFLTeam:    strings.ToUpper(textField(player, "team", "nflTeam", "nfl_team")),
			RookieYear: intField(player, "draft_year", "draftYear", "rookieYear", "rookie_year"),
			Birthdate:  birthdate,
		}
	}
	return result
}

// PlayerIDs returns all IDs in an MFL get_players response.
func PlayerIDs(payload map[string]any) []string {
	catalog := parseCatalog(payload)
	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// LeaguePlayerIDs returns rostered and free-agent IDs relevant to a league.
func LeaguePlayerIDs(rostersPayload, freeAgentsPayload map[string]any) []string {
	ids := make(map[string]struct{})
	rosters := envelope(rostersPayload, "rosters")
	for _, franchiseValue := range values(rosters["franchise"]) {
		franchise, ok := object(franchiseValue)
		if !ok {
			continue
		}
		for _, playerValue := range values(franchise["player"]) {
			entry, ok := object(playerValue)
			if ok {
				if id := textField(entry, "id"); id != "" {
					ids[id] = struct{}{}
				}
			}
		}
	}
	walkObjects(envelope(freeAgentsPayload, "freeAgents"), func(item map[string]any) {
		if id := textField(item, "id"); id != "" {
			ids[id] = struct{}{}
		}
	})
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	sort.Strings(result)
	return result
}

func normalizeLeague(payload map[string]any, fallback source.League, franchiseID string) (source.League, string) {
	league := fallback
	root := envelope(payload, "league")
	if value := textField(root, "id"); value != "" {
		league.ID = value
	}
	if value := textField(root, "name"); value != "" {
		league.Name = value
	}
	if value, ok := numberField(root, "salaryCapAmount", "salary_cap", "salaryCap"); ok && value > 0 {
		league.SalaryCap = value
	}
	if value := intField(root, "rosterSize", "activeRosterLimit", "active_roster_limit"); value > 0 {
		league.ActiveRosterLimit = value
	}
	if value := intField(root, "injuredReserve", "injuredReserveLimit", "injured_reserve_limit"); value >= 0 && hasAny(root, "injuredReserve", "injuredReserveLimit", "injured_reserve_limit") {
		league.InjuredReserveLimit = value
	}
	if value := intField(root, "taxiSquad", "taxiSquadLimit", "taxi_squad_limit"); value >= 0 && hasAny(root, "taxiSquad", "taxiSquadLimit", "taxi_squad_limit") {
		league.TaxiSquadLimit = value
	}
	franchiseName := ""
	if franchises, ok := object(root["franchises"]); ok {
		for _, item := range values(franchises["franchise"]) {
			franchise, ok := object(item)
			if ok && textField(franchise, "id") == franchiseID {
				franchiseName = textField(franchise, "name")
				break
			}
		}
	}
	return league, franchiseName
}

func normalizeRoster(payload map[string]any, franchiseID string, catalog map[string]catalogPlayer, existing []source.Player, multipliers map[string]float64) ([]source.Player, error) {
	root := envelope(payload, "rosters")
	franchises := values(root["franchise"])
	if len(franchises) == 0 && textField(root, "id") != "" {
		franchises = []any{root}
	}
	var selected map[string]any
	for _, item := range franchises {
		franchise, ok := object(item)
		if ok && textField(franchise, "id") == franchiseID {
			selected = franchise
			break
		}
	}
	if selected == nil {
		return nil, fmt.Errorf("MFL rosters response did not contain franchise %s", franchiseID)
	}
	existingByID := make(map[string]source.Player, len(existing))
	for _, player := range existing {
		existingByID[player.ID] = player
	}
	roster := make([]source.Player, 0)
	for _, item := range values(selected["player"]) {
		entry, ok := object(item)
		if !ok {
			continue
		}
		id := textField(entry, "id")
		if id == "" {
			continue
		}
		player := existingByID[id]
		player.ID = id
		if metadata, ok := catalog[id]; ok {
			player.Name = metadata.Name
			player.Position = metadata.Position
			player.NFLTeam = metadata.NFLTeam
			if metadata.RookieYear != 0 {
				player.RookieYear = metadata.RookieYear
			}
		}
		if player.Name == "" {
			player.Name = id
		}
		if value, ok := numberField(entry, "salary"); ok {
			player.Salary = value
		}
		player.Status = normalizeStatus(textField(entry, "status"))
		player.CurrentCapHit = round(player.Salary*multipliers[player.Status], 2)
		roster = append(roster, player)
	}
	if len(roster) == 0 {
		return nil, fmt.Errorf("MFL rosters response contained no players for franchise %s", franchiseID)
	}
	sort.Slice(roster, func(i, j int) bool {
		if roster[i].Status != roster[j].Status {
			return roster[i].Status < roster[j].Status
		}
		return roster[i].Name < roster[j].Name
	})
	return roster, nil
}

func summarizeFranchise(base source.Franchise, league source.League, roster []source.Player, adjustment float64) source.Franchise {
	franchise := base
	franchise.ActivePlayers = 0
	franchise.InjuredReservePlayers = 0
	franchise.TaxiSquadPlayers = 0
	franchise.ActiveSalary = 0
	franchise.InjuredReserveCapHit = 0
	franchise.TaxiSquadCapHit = 0
	for _, player := range roster {
		switch player.Status {
		case "INJURED_RESERVE":
			franchise.InjuredReservePlayers++
			franchise.InjuredReserveCapHit += player.CurrentCapHit
		case "TAXI_SQUAD":
			franchise.TaxiSquadPlayers++
			franchise.TaxiSquadCapHit += player.CurrentCapHit
		default:
			franchise.ActivePlayers++
			franchise.ActiveSalary += player.CurrentCapHit
		}
	}
	franchise.ActiveSalary = round(franchise.ActiveSalary, 2)
	franchise.InjuredReserveCapHit = round(franchise.InjuredReserveCapHit, 2)
	franchise.TaxiSquadCapHit = round(franchise.TaxiSquadCapHit, 2)
	franchise.TotalCapHit = round(franchise.ActiveSalary+franchise.InjuredReserveCapHit+franchise.TaxiSquadCapHit+adjustment, 2)
	franchise.CurrentCapSpace = round(league.SalaryCap-franchise.TotalCapHit, 2)
	return franchise
}

func salaryAdjustment(payload map[string]any, franchiseID string) float64 {
	total := 0.0
	walkObjects(payload, func(item map[string]any) {
		if textField(item, "franchise_id", "franchiseId", "franchise") != franchiseID {
			return
		}
		if amount, ok := numberField(item, "amount"); ok {
			total += amount
		}
	})
	return total
}

func birthdates(payload map[string]any) map[string]int64 {
	result := make(map[string]int64)
	walkObjects(payload, func(item map[string]any) {
		id := textField(item, "id", "player_id", "playerId")
		date := textField(item, "birthdate", "birthDate", "dob")
		if id == "" || date == "" {
			return
		}
		if timestamp, err := strconv.ParseInt(date, 10, 64); err == nil && timestamp > 0 {
			result[id] = timestamp
			return
		}
		for _, layout := range []string{"2006-01-02", "01/02/2006", "1/2/2006", "Jan 2, 2006"} {
			if parsed, err := time.ParseInLocation(layout, date, time.UTC); err == nil {
				result[id] = parsed.Unix()
				return
			}
		}
	})
	return result
}

func availableRookieSummary(payload map[string]any, catalog map[string]catalogPlayer, year int, excluded map[string]bool) map[string]any {
	ids := make(map[string]bool)
	walkObjects(envelope(payload, "freeAgents"), func(item map[string]any) {
		id := textField(item, "id")
		if metadata, ok := catalog[id]; ok && metadata.RookieYear == year && !excluded[id] {
			ids[id] = true
		}
	})
	counts := make(map[string]int)
	for id := range ids {
		if position := catalog[id].Position; position != "" {
			counts[position]++
		}
	}
	return map[string]any{
		"mfl_draft_year":                      year,
		"unrostered_supported_position_count": len(ids),
		"counts_by_position":                  counts,
		"player_ids":                          sortedKeys(ids),
		"note":                                "Live MFL free-agent rookies; multi-year projections are still required before ranking them.",
	}
}

func ownedCurrentPicks(payload map[string]any, franchiseID string, base BaseDocument, franchiseNames map[string]string, teamCount int) ([]source.Pick, bool) {
	root := envelope(payload, "assets")
	var selected map[string]any
	walkObjects(root, func(item map[string]any) {
		if selected == nil && textField(item, "id", "franchise_id", "franchiseId") == franchiseID {
			if hasAny(item, "currentYearDraftPick", "currentYearDraftPicks", "draftPick", "draftPicks") {
				selected = item
			}
		}
	})
	if selected == nil {
		return nil, false
	}
	baseByLabel := make(map[string]source.Pick, len(base.Snapshot.Draft.CurrentYearPicks))
	for _, pick := range base.Snapshot.Draft.CurrentYearPicks {
		baseByLabel[pick.Pick] = pick
	}
	seen := make(map[string]bool)
	picks := make([]source.Pick, 0)
	walkNamed(selected, func(key string, item map[string]any) {
		lowerKey := strings.ToLower(key)
		id := textField(item, "id")
		if strings.HasPrefix(id, "FP_") || strings.Contains(lowerKey, "future") {
			return
		}
		roundNumber, pickNumber := draftCoordinates(item, id)
		if roundNumber <= 0 || pickNumber <= 0 || (!strings.Contains(lowerKey, "draftpick") && !strings.HasPrefix(id, "DP_")) {
			return
		}
		label := fmt.Sprintf("%d.%02d", roundNumber, pickNumber)
		if seen[label] {
			return
		}
		seen[label] = true
		pick := baseByLabel[label]
		pick.Pick = label
		pick.Overall = intField(item, "overall", "overallPick")
		if pick.Overall == 0 && teamCount > 0 {
			pick.Overall = (roundNumber-1)*teamCount + pickNumber
		}
		if pick.Salary == 0 {
			pick.Salary = salaryForPick(base.Raw, roundNumber, pickNumber)
		}
		originalID := textField(item, "originalFranchise", "original_franchise", "originalPickFor", "originalOwner")
		if name := franchiseNames[originalID]; name != "" {
			pick.OriginalOwner = name
		}
		picks = append(picks, pick)
	})
	if len(picks) == 0 {
		return nil, false
	}
	sort.Slice(picks, func(i, j int) bool { return picks[i].Overall < picks[j].Overall })
	return picks, true
}

func ownedPicksFromDraftResults(payload map[string]any, franchiseID string, base BaseDocument, franchiseNames map[string]string) ([]source.Pick, bool) {
	baseByLabel := make(map[string]source.Pick, len(base.Snapshot.Draft.CurrentYearPicks))
	for _, pick := range base.Snapshot.Draft.CurrentYearPicks {
		baseByLabel[pick.Pick] = pick
	}
	seen := make(map[string]bool)
	picks := make([]source.Pick, 0)
	overall := 0
	sawOwnedPick := false
	walkObjects(envelope(payload, "draftResults"), func(item map[string]any) {
		roundNumber := intField(item, "round")
		pickNumber := intField(item, "pick")
		owner := textField(item, "franchise")
		if roundNumber <= 0 || pickNumber <= 0 || owner == "" {
			return
		}
		overall++
		if owner != franchiseID {
			return
		}
		sawOwnedPick = true
		if textField(item, "player") != "" {
			return
		}
		label := fmt.Sprintf("%d.%02d", roundNumber, pickNumber)
		if seen[label] {
			return
		}
		seen[label] = true
		pick := baseByLabel[label]
		pick.Pick = label
		pick.Overall = overall
		if pick.Salary == 0 {
			pick.Salary = salaryForPick(base.Raw, roundNumber, pickNumber)
		}
		if pick.OriginalOwner == "" {
			pick.OriginalOwner = originalOwnerFromComments(textField(item, "comments"))
			if pick.OriginalOwner == "" {
				pick.OriginalOwner = franchiseNames[owner]
			}
		}
		picks = append(picks, pick)
	})
	if !sawOwnedPick {
		return nil, false
	}
	sort.Slice(picks, func(i, j int) bool { return picks[i].Overall < picks[j].Overall })
	return picks, true
}

func futurePickInventory(payload map[string]any, franchiseID string, franchiseNames map[string]string) ([]any, bool) {
	root := envelope(payload, "futureDraftPicks")
	searchRoot := any(root)
	walkObjects(root, func(item map[string]any) {
		if textField(item, "id", "franchise", "franchise_id", "franchiseId") == franchiseID &&
			hasAny(item, "futureDraftPick", "futureDraftPicks", "draftPick", "draftPicks") {
			searchRoot = item
		}
	})
	byYearOwner := make(map[string]map[int]bool)
	walkObjects(searchRoot, func(item map[string]any) {
		owner := textField(item, "franchise", "franchise_id", "franchiseId", "owner")
		if owner != "" && owner != franchiseID {
			return
		}
		year := intField(item, "year")
		roundNumber := intField(item, "round")
		if year == 0 || roundNumber == 0 {
			if id := textField(item, "id"); strings.HasPrefix(id, "FP_") {
				parts := strings.Split(id, "_")
				if len(parts) == 4 {
					year, _ = strconv.Atoi(parts[2])
					roundNumber, _ = strconv.Atoi(parts[3])
				}
			}
		}
		if year == 0 || roundNumber == 0 {
			return
		}
		originalID := textField(item, "originalFranchise", "original_franchise", "originalPickFor")
		key := strconv.Itoa(year) + "|" + originalID
		if byYearOwner[key] == nil {
			byYearOwner[key] = make(map[int]bool)
		}
		byYearOwner[key][roundNumber] = true
	})
	if len(byYearOwner) == 0 {
		return nil, false
	}
	keys := make([]string, 0, len(byYearOwner))
	for key := range byYearOwner {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]any, 0, len(keys))
	for _, key := range keys {
		parts := strings.Split(key, "|")
		year, _ := strconv.Atoi(parts[0])
		rounds := sortedInts(byYearOwner[key])
		owner := franchiseNames[parts[1]]
		if owner == "" {
			owner = parts[1]
		}
		result = append(result, map[string]any{"year": year, "rounds": rounds, "original_owner": owner})
	}
	return result, true
}

func draftStatus(payload map[string]any, fallback string) (string, string) {
	root := envelope(payload, "draftResults")
	status := textField(root, "status", "draftStatus", "state")
	message := textField(root, "message", "statusMessage")
	if status == "" {
		walkObjects(root, func(item map[string]any) {
			if status == "" {
				status = textField(item, "status", "draftStatus", "state")
			}
		})
	}
	if status == "" {
		totalPicks := 0
		completedPicks := 0
		walkObjects(root, func(item map[string]any) {
			if intField(item, "round") <= 0 || intField(item, "pick") <= 0 || !hasAny(item, "player") {
				return
			}
			totalPicks++
			if textField(item, "player") != "" {
				completedPicks++
			}
		})
		switch {
		case totalPicks > 0 && completedPicks == totalPicks:
			status = "completed"
		case completedPicks > 0:
			status = "in_progress"
		case totalPicks > 0:
			status = "scheduled"
		default:
			status = fallback
		}
	}
	status = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(status), " ", "_"))
	return status, message
}

func franchiseNameMap(payload map[string]any) map[string]string {
	result := make(map[string]string)
	root := envelope(payload, "league")
	if franchises, ok := object(root["franchises"]); ok {
		for _, item := range values(franchises["franchise"]) {
			franchise, ok := object(item)
			if ok {
				result[textField(franchise, "id")] = textField(franchise, "name")
			}
		}
	}
	return result
}

func teamCount(payload map[string]any, fallback int) int {
	root := envelope(payload, "league")
	if franchises, ok := object(root["franchises"]); ok {
		if count := intField(franchises, "count"); count > 0 {
			return count
		}
		if count := len(values(franchises["franchise"])); count > 0 {
			return count
		}
	}
	return fallback
}

func draftCoordinates(item map[string]any, id string) (int, int) {
	roundNumber := intField(item, "round")
	pickNumber := intField(item, "pick")
	if (roundNumber == 0 || pickNumber == 0) && strings.HasPrefix(id, "DP_") {
		parts := strings.Split(id, "_")
		if len(parts) >= 3 {
			roundNumber, _ = strconv.Atoi(parts[1])
			pickNumber, _ = strconv.Atoi(parts[2])
			roundNumber++
			pickNumber++
		}
	}
	return roundNumber, pickNumber
}

func originalOwnerFromComments(comments string) string {
	marker := "traded from "
	lower := strings.ToLower(comments)
	start := strings.Index(lower, marker)
	if start < 0 {
		return ""
	}
	value := strings.TrimSpace(comments[start+len(marker):])
	value = strings.TrimSuffix(value, "]")
	value = strings.TrimSuffix(value, ".")
	return strings.TrimSpace(value)
}

func salaryForPick(raw map[string]any, roundNumber, pickNumber int) float64 {
	draft, ok := object(raw["draft"])
	if !ok {
		return 0
	}
	for _, value := range values(draft["rookie_salary_schedule"]) {
		entry, ok := object(value)
		if !ok {
			continue
		}
		rangeText := strings.ToLower(textField(entry, "selection_range"))
		salary, _ := numberField(entry, "salary")
		label := fmt.Sprintf("%d.%02d", roundNumber, pickNumber)
		if strings.Contains(rangeText, "-") {
			bounds := strings.SplitN(rangeText, "-", 2)
			if len(bounds) == 2 && label >= bounds[0] && label <= bounds[1] {
				return salary
			}
			continue
		}
		if rangeText == fmt.Sprintf("round %d", roundNumber) || (rangeText == "round 4+" && roundNumber >= 4) {
			return salary
		}
	}
	return 0
}

func envelope(payload map[string]any, key string) map[string]any {
	if nested, ok := object(payload[key]); ok {
		return nested
	}
	return payload
}

func object(value any) (map[string]any, bool) {
	result, ok := value.(map[string]any)
	return result, ok
}

func values(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case map[string]any:
		return []any{typed}
	case nil:
		return nil
	default:
		return []any{typed}
	}
}

func textField(item map[string]any, names ...string) string {
	for _, name := range names {
		if value, exists := item[name]; exists {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
			case json.Number:
				return typed.String()
			case float64:
				return strconv.FormatFloat(typed, 'f', -1, 64)
			}
		}
	}
	return ""
}

func numberField(item map[string]any, names ...string) (float64, bool) {
	for _, name := range names {
		if value, exists := item[name]; exists {
			if parsed, ok := floatValue(value); ok {
				return parsed, true
			}
		}
	}
	return 0, false
}

func floatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func intField(item map[string]any, names ...string) int {
	value, ok := numberField(item, names...)
	if !ok {
		return 0
	}
	return int(value)
}

func hasAny(item map[string]any, names ...string) bool {
	for _, name := range names {
		if _, ok := item[name]; ok {
			return true
		}
	}
	return false
}

func walkObjects(value any, visit func(map[string]any)) {
	switch typed := value.(type) {
	case map[string]any:
		visit(typed)
		for _, child := range typed {
			walkObjects(child, visit)
		}
	case []any:
		for _, child := range typed {
			walkObjects(child, visit)
		}
	}
}

func walkNamed(value any, visit func(string, map[string]any)) {
	var walk func(string, any)
	walk = func(key string, current any) {
		switch typed := current.(type) {
		case map[string]any:
			visit(key, typed)
			for childKey, child := range typed {
				walk(childKey, child)
			}
		case []any:
			for _, child := range typed {
				walk(key, child)
			}
		}
	}
	walk("", value)
}

func displayName(value string) string {
	parts := strings.SplitN(value, ",", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1]) + " " + strings.TrimSpace(parts[0])
	}
	return value
}

func normalizeStatus(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "IR", "INJURED_RESERVE":
		return "INJURED_RESERVE"
	case "TS", "TAXI", "TAXI_SQUAD":
		return "TAXI_SQUAD"
	default:
		return "ROSTER"
	}
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func sortedInts(values map[int]bool) []int {
	result := make([]int, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}

func round(value float64, places int) float64 {
	scale := math.Pow10(places)
	return math.Round(value*scale) / scale
}
