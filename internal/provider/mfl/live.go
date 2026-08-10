package mflsync

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

type LiveDraftInfo struct {
	Status           string   `json:"status"`
	Message          string   `json:"message"`
	OnClockFranchise string   `json:"on_clock_franchise,omitempty"`
	Round            int      `json:"round,omitempty"`
	Pick             int      `json:"pick,omitempty"`
	DraftedPlayerIDs []string `json:"drafted_player_ids"`
	CompletedPicks   []string `json:"completed_picks"`
}

func parseLiveDraft(payload map[string]any) (LiveDraftInfo, error) {
	xmlText := textField(payload, "xml")
	if xmlText == "" {
		return LiveDraftInfo{}, fmt.Errorf("structured response did not contain xml")
	}
	decoder := xml.NewDecoder(strings.NewReader(xmlText))
	info := LiveDraftInfo{DraftedPlayerIDs: []string{}, CompletedPicks: []string{}}
	drafted := make(map[string]bool)
	totalPicks := 0
	completedPicks := 0
	paused := false
	stopped := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return LiveDraftInfo{}, fmt.Errorf("decode live draft XML: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		attributes := make(map[string]string, len(start.Attr))
		for _, attribute := range start.Attr {
			attributes[attribute.Name.Local] = attribute.Value
		}
		switch start.Name.Local {
		case "draftResults":
			info.OnClockFranchise = attributes["franchise_id"]
			info.Round, _ = strconv.Atoi(attributes["round"])
			info.Pick, _ = strconv.Atoi(attributes["pick"])
			paused = attributes["paused"] == "1"
			stopped = attributes["stopped"] == "1"
		case "draftPick":
			totalPicks++
			if id := strings.TrimSpace(attributes["player"]); id != "" {
				completedPicks++
				drafted[id] = true
				roundNumber, _ := strconv.Atoi(attributes["round"])
				pickNumber, _ := strconv.Atoi(attributes["pick"])
				if roundNumber > 0 && pickNumber > 0 {
					info.CompletedPicks = append(info.CompletedPicks, fmt.Sprintf("%d.%02d", roundNumber, pickNumber))
				}
			}
		}
	}
	for id := range drafted {
		info.DraftedPlayerIDs = append(info.DraftedPlayerIDs, id)
	}
	sort.Strings(info.DraftedPlayerIDs)
	sort.Strings(info.CompletedPicks)
	switch {
	case totalPicks > 0 && completedPicks == totalPicks:
		info.Status = "completed"
	case paused:
		info.Status = "paused"
	case stopped:
		info.Status = "stopped"
	case completedPicks > 0:
		info.Status = "in_progress"
	case totalPicks > 0:
		info.Status = "scheduled"
	}
	if info.Round > 0 && info.Pick > 0 && info.Status != "completed" {
		info.Message = fmt.Sprintf("On the clock: franchise %s at %d.%02d", info.OnClockFranchise, info.Round, info.Pick)
	}
	return info, nil
}
