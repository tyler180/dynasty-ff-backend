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
