// Package tyre models how lap time changes with tyre wear and fuel load.
package tyre

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// Compound describes one tyre compound's pace and wear.
//
// Lap time on a set that has already done `age` laps:
//
//	Base + Lin*age + Quad*age² + cliff
//
// where cliff adds CliffRate seconds for every lap past CliffAge, the point
// where the tyre "falls off" and gets much slower.
type Compound struct {
	Base      float64 `json:"base"`       // seconds on a fresh set with an empty tank
	Lin       float64 `json:"lin"`        // seconds lost per lap of age
	Quad      float64 `json:"quad"`       // extra loss that grows with age²
	CliffAge  int     `json:"cliff_age"`  // 0 means no cliff
	CliffRate float64 `json:"cliff_rate"` // seconds per lap past the cliff
}

// Race holds track-level settings.
type Race struct {
	Laps         int     `json:"laps"`
	FuelPerLap   float64 `json:"fuel_per_lap"`  // seconds per lap of fuel still on board
	PitLoss      float64 `json:"pit_loss"`      // seconds lost driving through the pit lane and stopping
	MinCompounds int     `json:"min_compounds"` // dry-race rule: must use at least this many compounds
	MaxStops     int     `json:"max_stops"`
}

// Model is a race plus the compounds available for it.
type Model struct {
	Race      Race                `json:"race"`
	Compounds map[string]Compound `json:"compounds"`
}

// Names returns compound names in a stable order.
func (m *Model) Names() []string {
	names := make([]string, 0, len(m.Compounds))
	for n := range m.Compounds {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// LapTime is the time for race lap `lap` (1-based) on compound c whose set
// has already done `age` laps.
func (m *Model) LapTime(c Compound, age, lap int) float64 {
	a := float64(age)
	t := c.Base + c.Lin*a + c.Quad*a*a
	if c.CliffAge > 0 && age > c.CliffAge {
		t += c.CliffRate * float64(age-c.CliffAge)
	}
	t += m.Race.FuelPerLap * float64(m.Race.Laps-lap)
	return t
}

// Validate catches settings that would make the optimizer meaningless.
func (m *Model) Validate() error {
	r := m.Race
	switch {
	case r.Laps < 2 || r.Laps > 200:
		return fmt.Errorf("laps must be between 2 and 200, got %d", r.Laps)
	case r.PitLoss < 0:
		return fmt.Errorf("pit_loss can't be negative")
	case r.MaxStops < 0 || r.MaxStops > 5:
		return fmt.Errorf("max_stops must be between 0 and 5, got %d", r.MaxStops)
	case len(m.Compounds) == 0:
		return fmt.Errorf("no compounds")
	case len(m.Compounds) > 8:
		return fmt.Errorf("at most 8 compounds")
	case r.MinCompounds > len(m.Compounds):
		return fmt.Errorf("min_compounds=%d but only %d compounds given", r.MinCompounds, len(m.Compounds))
	}
	return nil
}

// Load reads a model from JSON.
func Load(path string) (*Model, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Model
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return &m, m.Validate()
}

// Save writes a model as indented JSON.
func (m *Model) Save(path string) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
