package dynastyprocess

import (
	"strings"
	"testing"
)

func TestDecodePlayers(t *testing.T) {
	players, err := decode(strings.NewReader("mfl_id,name,db_season,gsis_id,birthdate,draft_year,draft_round,draft_pick\n13589,Josh Allen,2026,00-0034857,1996-05-21,2018,1,7\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(players) != 1 || players[0].MFLID != "13589" || players[0].DraftPick != 7 {
		t.Fatalf("players = %+v", players)
	}
}
