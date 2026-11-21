package lambdaapp

import "time"

// seasonStartMonth is the first month in which nflverse publishes the new
// season's game-level datasets. Earlier months belong to the prior season,
// which also covers January and February playoff games.
const seasonStartMonth = time.September

// currentNFLSeason returns the NFL season that is in progress, or most
// recently completed, at the given instant.
func currentNFLSeason(now time.Time) int {
	now = now.UTC()
	if now.Month() >= seasonStartMonth {
		return now.Year()
	}
	return now.Year() - 1
}

// syncSeason keeps an explicitly requested season and otherwise derives the
// current NFL season, so scheduled syncs do not need a pinned year.
func (h *Handler) syncSeason(requested int) int {
	if requested != 0 {
		return requested
	}
	now := time.Now
	if h.now != nil {
		now = h.now
	}
	return currentNFLSeason(now())
}
