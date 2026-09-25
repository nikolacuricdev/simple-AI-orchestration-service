package weather_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/nikolacuricdev/simple-AI-orchestration-service/internal/weather"
)

func TestLookupNormalisesModelSuppliedCityNames(t *testing.T) {
	// An LLM will hand over " paris ", "PARIS" or "Paris" interchangeably.
	for _, city := range []string{"Paris", "paris", "  PARIS  "} {
		report, err := weather.Lookup(city)
		if err != nil {
			t.Fatalf("Lookup(%q): %v", city, err)
		}
		if report.City != "Paris" {
			t.Errorf("Lookup(%q).City = %q, want %q", city, report.City, "Paris")
		}
	}
}

func TestLookupUnknownCity(t *testing.T) {
	_, err := weather.Lookup("Atlantis")
	if !errors.Is(err, weather.ErrUnknownCity) {
		t.Fatalf("err = %v, want ErrUnknownCity", err)
	}
	// The error lists the valid options so the model can retry with one.
	if got := err.Error(); !strings.Contains(got, "Belgrade") {
		t.Errorf("error %q should list the known cities", got)
	}
}

func TestLookupEmptyCity(t *testing.T) {
	if _, err := weather.Lookup("   "); !errors.Is(err, weather.ErrUnknownCity) {
		t.Fatalf("err = %v, want ErrUnknownCity", err)
	}
}
