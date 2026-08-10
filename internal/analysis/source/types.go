package source

type Snapshot struct {
	SnapshotDate         string            `json:"snapshot_date"`
	Purpose              string            `json:"purpose"`
	League               League            `json:"league"`
	Franchise            Franchise         `json:"franchise"`
	Roster               []Player          `json:"roster"`
	HistoricalPoints     HistoricalPoints  `json:"historical_points"`
	ReplacementLevels    ReplacementLevels `json:"replacement_levels"`
	BirthdatesUnix       map[string]int64  `json:"birthdates_unix"`
	Projections          Projections       `json:"projections"`
	Draft                Draft             `json:"draft"`
	SourceReconciliation []string          `json:"source_reconciliation"`
}

type League struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	SalaryCap           float64 `json:"salary_cap"`
	ActiveRosterLimit   int     `json:"active_roster_limit"`
	InjuredReserveLimit int     `json:"injured_reserve_limit"`
	TaxiSquadLimit      int     `json:"taxi_squad_limit"`
}

type Franchise struct {
	ID                    string  `json:"id"`
	Name                  string  `json:"name"`
	ActivePlayers         int     `json:"active_players"`
	InjuredReservePlayers int     `json:"injured_reserve_players"`
	TaxiSquadPlayers      int     `json:"taxi_squad_players"`
	ActiveSalary          float64 `json:"active_salary"`
	InjuredReserveCapHit  float64 `json:"injured_reserve_cap_hit"`
	TaxiSquadCapHit       float64 `json:"taxi_squad_cap_hit"`
	TotalCapHit           float64 `json:"total_cap_hit"`
	CurrentCapSpace       float64 `json:"current_cap_space"`
}

type Player struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Position      string  `json:"position"`
	NFLTeam       string  `json:"nfl_team"`
	Salary        float64 `json:"salary"`
	Status        string  `json:"status"`
	CurrentCapHit float64 `json:"current_cap_hit,omitempty"`
	RookieYear    int     `json:"rookie_year,omitempty"`
}

type HistoricalPoints struct {
	Source     string             `json:"source,omitempty"`
	Season     int                `json:"season,omitempty"`
	ByPlayerID map[string]float64 `json:"by_player_id,omitempty"`
	Seasons    []HistoricalSeason `json:"seasons,omitempty"`
}

type HistoricalSeason struct {
	Season                int                `json:"season"`
	ByPlayerID            map[string]float64 `json:"by_player_id"`
	GamesPlayedByPlayerID map[string]int     `json:"games_played_by_player_id,omitempty"`
}

type ReplacementLevels struct {
	Source                  string             `json:"source,omitempty"`
	Method                  string             `json:"method,omitempty"`
	MinimumHistoricalGames  int                `json:"minimum_historical_games,omitempty"`
	PointsPerGameByPosition map[string]float64 `json:"points_per_game_by_position,omitempty"`
}

type Projections struct {
	Season     int                `json:"season"`
	Source     string             `json:"source"`
	ByPlayerID map[string]float64 `json:"by_player_id"`
}

type Draft struct {
	Status                 string                 `json:"status"`
	MFLMessage             string                 `json:"mfl_message"`
	AvailabilityPollWindow AvailabilityPollWindow `json:"availability_poll_window"`
	CurrentYearPicks       []Pick                 `json:"current_year_picks"`
}

type AvailabilityPollWindow struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type Pick struct {
	Pick          string  `json:"pick"`
	Overall       int     `json:"overall"`
	Salary        float64 `json:"salary"`
	OriginalOwner string  `json:"original_owner"`
}

type Analysis struct {
	SnapshotDate         string               `json:"snapshot_date"`
	League               string               `json:"league"`
	Team                 string               `json:"team"`
	Cap                  CapSummary           `json:"cap"`
	Roster               RosterSummary        `json:"roster"`
	Draft                DraftSummary         `json:"draft"`
	TaxiCompliance       TaxiCompliance       `json:"taxi_compliance"`
	ComplianceScenarios  []ComplianceScenario `json:"compliance_scenarios"`
	HistoricalEfficiency HistoricalEfficiency `json:"historical_efficiency"`
	DropEvaluation       DropEvaluation       `json:"drop_evaluation"`
	Warnings             []string             `json:"warnings"`
}

type AnalysisOptions struct {
	CapReliefTarget    float64
	ProjectionFallback string
}

type CapSummary struct {
	Used  float64 `json:"used"`
	Limit float64 `json:"limit"`
	Space float64 `json:"space"`
}

type RosterSummary struct {
	Active         SlotSummary `json:"active"`
	InjuredReserve SlotSummary `json:"injured_reserve"`
	Taxi           SlotSummary `json:"taxi"`
}

type SlotSummary struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
	Open  int `json:"open"`
}

type DraftSummary struct {
	Status              string                 `json:"status"`
	AvailabilityWindow  AvailabilityPollWindow `json:"availability_window"`
	Picks               []PickAssessment       `json:"picks"`
	PickCount           int                    `json:"pick_count"`
	TotalSalaryIfActive float64                `json:"total_salary_if_all_active"`
}

type PickAssessment struct {
	Pick               string  `json:"pick"`
	Overall            int     `json:"overall"`
	Salary             float64 `json:"salary"`
	FitsActiveNow      bool    `json:"fits_active_now"`
	ActiveCapShortfall float64 `json:"active_cap_shortfall"`
	FitsTaxiNow        bool    `json:"fits_taxi_now"`
}

type PlayerSummary struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Position   string  `json:"position"`
	Salary     float64 `json:"salary"`
	RookieYear int     `json:"rookie_year,omitempty"`
}

type TaxiCompliance struct {
	CurrentYear          int             `json:"current_year"`
	MustLeaveTaxi        []PlayerSummary `json:"must_leave_taxi"`
	UnknownEligibility   []PlayerSummary `json:"unknown_eligibility"`
	SlotsOpenedAfterMove int             `json:"slots_opened_after_move"`
}

type ComplianceScenario struct {
	Name                string          `json:"name"`
	Promote             []PlayerSummary `json:"promote"`
	RemoveOrTrade       []PlayerSummary `json:"remove_or_trade"`
	FirstPicksToTaxi    []string        `json:"first_picks_to_taxi"`
	ResultingActive     int             `json:"resulting_active"`
	ResultingTaxi       int             `json:"resulting_taxi"`
	ResultingCapHit     float64         `json:"resulting_cap_hit"`
	AdditionalCapRelief float64         `json:"additional_cap_relief_required"`
	RosterLegal         bool            `json:"roster_legal"`
}

type EfficiencyPlayer struct {
	Name                    string  `json:"name"`
	Position                string  `json:"position"`
	Salary                  float64 `json:"salary"`
	HistoricalPointsPerGame float64 `json:"historical_points_per_game"`
	PointsPerGamePerSalary  float64 `json:"points_per_game_per_salary"`
}

type HistoricalEfficiency struct {
	Seasons        []int              `json:"seasons"`
	Method         string             `json:"method"`
	MostEfficient  []EfficiencyPlayer `json:"most_efficient"`
	LeastEfficient []EfficiencyPlayer `json:"least_efficient"`
	Caution        string             `json:"caution"`
}

type DropEvaluation struct {
	Available         bool            `json:"available"`
	ProjectionSeason  int             `json:"projection_season,omitempty"`
	ProjectionSource  string          `json:"projection_source,omitempty"`
	ProductionMetric  string          `json:"production_metric,omitempty"`
	ReplacementSource string          `json:"replacement_source,omitempty"`
	CapReliefTarget   float64         `json:"cap_relief_target"`
	BestForTarget     *DropCandidate  `json:"best_for_target,omitempty"`
	Candidates        []DropCandidate `json:"candidates"`
	DropCandidates    []DropCandidate `json:"drop_candidates,omitempty"`
	TradeFirst        []DropCandidate `json:"trade_first,omitempty"`
	HoldDevelop       []DropCandidate `json:"hold_develop,omitempty"`
	MissingPlayerIDs  []string        `json:"missing_player_ids,omitempty"`
	Method            string          `json:"method"`
	Caution           string          `json:"caution"`
}

type DropCandidate struct {
	Rank                     int     `json:"rank"`
	PlayerID                 string  `json:"player_id"`
	Name                     string  `json:"name"`
	Position                 string  `json:"position"`
	Age                      int     `json:"age"`
	SalaryCapRelief          float64 `json:"salary_cap_relief"`
	ProductionValue          float64 `json:"production_value"`
	ProductionSeasons        []int   `json:"production_seasons,omitempty"`
	HistoricalGames          int     `json:"historical_games,omitempty"`
	CareerSeasons            int     `json:"career_seasons,omitempty"`
	AgeFactor                float64 `json:"age_factor"`
	DevelopmentFactor        float64 `json:"development_factor"`
	ReplacementPointsPerGame float64 `json:"replacement_points_per_game,omitempty"`
	ValueOverReplacement     float64 `json:"value_over_replacement,omitempty"`
	DynastyAdjustedVORP      float64 `json:"dynasty_adjusted_vorp,omitempty"`
	AgeAdjustedProduction    float64 `json:"age_adjusted_production"`
	DropScore                float64 `json:"drop_score"`
	Disposition              string  `json:"disposition"`
	DispositionReason        string  `json:"disposition_reason"`
}
