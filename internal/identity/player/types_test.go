package player

import "testing"

func TestExternalIDRequiresProviderAndValue(t *testing.T) {
	for _, id := range []ExternalID{
		{Value: "15751"},
		{Provider: ProviderMFL},
	} {
		if err := id.Validate(); err == nil {
			t.Fatalf("invalid external ID was accepted: %+v", id)
		}
	}
	if err := (ExternalID{Provider: ProviderMFL, Value: "15751"}).Validate(); err != nil {
		t.Fatalf("valid external ID was rejected: %v", err)
	}
}

func TestDeterministicIDIsStableAndProviderScoped(t *testing.T) {
	mfl, err := DeterministicID(ExternalID{Provider: ProviderMFL, Value: "17750"})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := DeterministicID(ExternalID{Provider: ProviderMFL, Value: "17750"})
	other, _ := DeterministicID(ExternalID{Provider: ProviderFantasyPros, Value: "17750"})
	if mfl == "" || mfl != again || mfl == other {
		t.Fatalf("deterministic IDs = %q, %q, %q", mfl, again, other)
	}
}
