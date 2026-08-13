package mflingest

import (
	"context"
	"testing"
	"time"
)

func TestServiceRejectsIncompleteConfiguration(t *testing.T) {
	service := Service{}
	_, err := service.Sync(context.Background(), Request{
		Year: 2026, LeagueID: "79286", FranchiseID: "0005", Timeout: time.Minute,
	})
	if err == nil {
		t.Fatal("expected configuration error")
	}
}
