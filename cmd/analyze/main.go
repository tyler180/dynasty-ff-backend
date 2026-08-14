package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	source "github.com/tyler180/dynasty-ff-models/analysis"
	"github.com/tyler180/dynasty-ff-backend/internal/app/draftanalysis"
	inputjson "github.com/tyler180/dynasty-ff-backend/internal/draftadapter/input"
	"github.com/tyler180/dynasty-ff-models/draft"
)

func main() {
	inputPath := flag.String("input", "", "path to an optimizer JSON input file, or - for stdin")
	sourcePath := flag.String("source", "", "optional local snapshot for non-MFL rules and historical enrichment")
	format := flag.String("format", "text", "source report format: text or json")
	capRelief := flag.Float64("cap-relief", 0, "minimum salary-cap relief for the best single-cut result")
	projectionFallback := flag.String("projection-fallback", "auto", "projection fallback for source analysis: auto, none, or historical")
	pretty := flag.Bool("pretty", true, "pretty-print JSON output")
	refreshMFL := flag.Bool("refresh-mfl", false, "refresh the source snapshot from MFL through its MCP server before analysis")
	command := flag.String("mcp-command", strings.TrimSpace(os.Getenv("MFL_MCP_COMMAND")), "path to the mfl-mcp executable; defaults to MFL_MCP_COMMAND")
	leagueID := flag.String("league", "", "MFL league ID; defaults to MFL_LEAGUE_ID or the source snapshot")
	franchiseID := flag.String("franchise", "", "MFL franchise ID; defaults to MFL_FRANCHISE_ID or the source snapshot")
	year := flag.Int("year", 0, "MFL season; defaults to MFL_YEAR, the source snapshot year, or the current year")
	projectionPath := flag.String("projections", "", "optional JSON projections file keyed by MFL player ID")
	leagueConfigPath := flag.String("league-config", "", "optional non-MFL league policy file; defaults to config/league-<id>.json when present")
	snapshotDate := flag.String("snapshot-date", "", "MFL snapshot date in YYYY-MM-DD; defaults to today")
	includeDraft := flag.Bool("draft", true, "refresh current picks, future picks, and draft status")
	liveDraft := flag.Bool("live-draft", false, "use MFL's near-real-time draft feed and exclude live selections from the rookie pool")
	timeout := flag.Duration("timeout", 3*time.Minute, "overall MFL MCP refresh timeout")
	exportSnapshot := flag.String("export-snapshot", "", "optional path for writing the refreshed source snapshot")
	var commandArguments stringList
	flag.Var(&commandArguments, "mcp-arg", "argument passed to the MCP executable; may be repeated")
	flag.Parse()

	if *refreshMFL || *sourcePath != "" {
		if *inputPath != "" {
			fail(fmt.Errorf("use either -input or MFL/source analysis, not both"))
		}
		resolvedYear := *year
		resolvedLeagueID := strings.TrimSpace(*leagueID)
		resolvedFranchiseID := strings.TrimSpace(*franchiseID)
		if *refreshMFL && resolvedYear == 0 {
			var err error
			resolvedYear, err = yearFromEnvironment()
			if err != nil {
				fail(err)
			}
		}
		if *refreshMFL {
			resolvedLeagueID = firstNonEmpty(resolvedLeagueID, strings.TrimSpace(os.Getenv("MFL_LEAGUE_ID")))
			resolvedFranchiseID = firstNonEmpty(resolvedFranchiseID, strings.TrimSpace(os.Getenv("MFL_FRANCHISE_ID")))
		}
		service := draftanalysis.NewService()
		result, err := service.Run(context.Background(), draftanalysis.Request{
			Load: sourceRefreshOptions{
				SourcePath:       *sourcePath,
				RefreshMFL:       *refreshMFL,
				Command:          *command,
				CommandArguments: commandArguments,
				LeagueID:         resolvedLeagueID,
				FranchiseID:      resolvedFranchiseID,
				Year:             resolvedYear,
				ProjectionPath:   strings.TrimSpace(*projectionPath),
				LeagueConfigPath: strings.TrimSpace(*leagueConfigPath),
				SnapshotDate:     strings.TrimSpace(*snapshotDate),
				IncludeDraft:     *includeDraft,
				LiveDraft:        *liveDraft,
				Timeout:          *timeout,
				ExportPath:       strings.TrimSpace(*exportSnapshot),
			},
			CapReliefTarget:    *capRelief,
			ProjectionFallback: *projectionFallback,
		})
		if err != nil {
			fail(err)
		}
		if result.Refresh != nil {
			fmt.Fprintf(os.Stderr, "dynasty: refreshed %d players with $%.2f cap usage; %d warning(s)\n",
				result.Refresh.RosterPlayers, result.Refresh.TotalCapHit, len(result.Refresh.Warnings))
		}
		writeAnalysis(result.Analysis, *format, *pretty)
		return
	}
	if *exportSnapshot != "" || *projectionPath != "" || *leagueConfigPath != "" || *liveDraft ||
		*leagueID != "" || *franchiseID != "" || *year != 0 || *snapshotDate != "" || len(commandArguments) > 0 {
		fail(fmt.Errorf("MFL refresh options require -refresh-mfl"))
	}
	path := *inputPath
	if path == "" {
		path = "-"
	}
	in, err := inputjson.Read(path)
	if err != nil {
		fail(err)
	}
	recommendation, err := draft.Recommend(in)
	if err != nil {
		fail(err)
	}

	encoder := json.NewEncoder(os.Stdout)
	if *pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(recommendation); err != nil {
		fail(fmt.Errorf("write output: %w", err))
	}
}

type sourceRefreshOptions = draftanalysis.LoadOptions

func loadSource(options sourceRefreshOptions) (source.Snapshot, error) {
	result, err := draftanalysis.NewLoader().Load(context.Background(), options)
	return result.Snapshot, err
}

func yearFromEnvironment() (int, error) {
	value := strings.TrimSpace(os.Getenv("MFL_YEAR"))
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 2000 || parsed > 2100 {
		return 0, fmt.Errorf("MFL_YEAR must be a season between 2000 and 2100")
	}
	return parsed, nil
}

func writeAnalysis(analysis source.Analysis, format string, pretty bool) {
	switch format {
	case "text":
		fmt.Print(source.FormatText(analysis))
	case "json":
		encoder := json.NewEncoder(os.Stdout)
		if pretty {
			encoder.SetIndent("", "  ")
		}
		if err := encoder.Encode(analysis); err != nil {
			fail(fmt.Errorf("write source analysis: %w", err))
		}
	default:
		fail(fmt.Errorf("unknown source report format %q; use text or json", format))
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

type stringList []string

func (values *stringList) String() string { return strings.Join(*values, ",") }

func (values *stringList) Set(value string) error {
	*values = append(*values, value)
	return nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "dynasty:", err)
	os.Exit(1)
}
