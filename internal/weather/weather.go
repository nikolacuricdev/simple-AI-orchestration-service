// Package weather is the domain behind the demo tool.
//
// The data is fake and deliberately deterministic: this project is about
// orchestration, not meteorology, and a reproducible tool makes the agent's
// behaviour reproducible too. The one thing that is not fake is the error
// path — an unknown city really fails, which is what lets the demo show a
// model recovering from a tool error.
package weather

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrUnknownCity is returned for a city the demo dataset does not cover.
var ErrUnknownCity = errors.New("unknown city")

// Report is one city's current conditions.
type Report struct {
	City         string `json:"city"`
	TemperatureC int    `json:"temperature_c"`
	Condition    string `json:"condition"`
}

// forecasts is the demo dataset, keyed by lowercased city name.
var forecasts = map[string]Report{
	"belgrade":  {City: "Belgrade", TemperatureC: 24, Condition: "sunny"},
	"berlin":    {City: "Berlin", TemperatureC: 17, Condition: "overcast"},
	"lisbon":    {City: "Lisbon", TemperatureC: 26, Condition: "clear"},
	"london":    {City: "London", TemperatureC: 14, Condition: "light rain"},
	"paris":     {City: "Paris", TemperatureC: 19, Condition: "partly cloudy"},
	"vienna":    {City: "Vienna", TemperatureC: 18, Condition: "cloudy"},
	"zagreb":    {City: "Zagreb", TemperatureC: 21, Condition: "sunny"},
	"amsterdam": {City: "Amsterdam", TemperatureC: 15, Condition: "windy"},
}

// Lookup returns the forecast for a city.
//
// The city name comes from an LLM, so it arrives with arbitrary casing and
// padding. Normalising before lookup is not politeness, it is the difference
// between the tool working and not.
func Lookup(city string) (Report, error) {
	key := strings.ToLower(strings.TrimSpace(city))
	if key == "" {
		return Report{}, fmt.Errorf("%w: no city given", ErrUnknownCity)
	}

	report, ok := forecasts[key]
	if !ok {
		// Listing the valid options in the error is what makes the failure
		// recoverable: the model reads this and can retry with a real city.
		return Report{}, fmt.Errorf("%w %q; known cities are: %s", ErrUnknownCity, city, strings.Join(Cities(), ", "))
	}
	return report, nil
}

// Cities returns the supported city names, sorted.
func Cities() []string {
	names := make([]string, 0, len(forecasts))
	for _, report := range forecasts {
		names = append(names, report.City)
	}
	sort.Strings(names)
	return names
}
