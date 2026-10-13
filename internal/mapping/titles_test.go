package mapping

import (
	"testing"
)

func TestTitleLookup(t *testing.T) {
	// 4156004D is "Cabela's Outdoor Adventure" in titles.json
	gotUpper := Title("4156004D")
	expected := "Cabela's Outdoor Adventure (4156004D)"
	if gotUpper != expected {
		t.Errorf("expected %q, got %q", expected, gotUpper)
	}

	// Test lowercase hex matching
	gotLower := Title("4156004d")
	if gotLower != expected {
		t.Errorf("expected lowercase to match %q, got %q", expected, gotLower)
	}

	// Unknown title ID
	gotUnknown := Title("99999999")
	if gotUnknown != "99999999" {
		t.Errorf("expected %q for unknown game, got %q", "99999999", gotUnknown)
	}
}
