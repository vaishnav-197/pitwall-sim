package tyre

import (
	"math"
	"path/filepath"
	"testing"
)

func model() *Model {
	return &Model{
		Race: Race{Laps: 10, FuelPerLap: 0.1, PitLoss: 20, MinCompounds: 2, MaxStops: 2},
		Compounds: map[string]Compound{
			"soft": {Base: 90, Lin: 0.1, Quad: 0.01, CliffAge: 3, CliffRate: 1},
			"hard": {Base: 91, Lin: 0.05},
		},
	}
}

func TestLapTime(t *testing.T) {
	m := model()
	soft := m.Compounds["soft"]
	cases := []struct {
		age, lap int
		want     float64
	}{
		{0, 10, 90},                    // fresh tyre, empty tank
		{0, 1, 90 + 0.9},               // 9 laps of fuel on board
		{2, 10, 90 + 0.2 + 0.04},       // wear, before cliff
		{5, 10, 90 + 0.5 + 0.25 + 2*1}, // two laps past the cliff
	}
	for _, c := range cases {
		if got := m.LapTime(soft, c.age, c.lap); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("LapTime(age=%d, lap=%d) = %v, want %v", c.age, c.lap, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	bad := []func(*Model){
		func(m *Model) { m.Race.Laps = 1 },
		func(m *Model) { m.Race.PitLoss = -1 },
		func(m *Model) { m.Race.MaxStops = 9 },
		func(m *Model) { m.Race.MinCompounds = 3 },
		func(m *Model) { m.Compounds = nil },
	}
	for i, f := range bad {
		m := model()
		f(m)
		if m.Validate() == nil {
			t.Errorf("case %d: want error", i)
		}
	}
	if err := model().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	if err := model().Save(path); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Compounds["soft"].CliffAge != 3 || m.Race.Laps != 10 {
		t.Fatalf("round trip lost data: %+v", m)
	}
}
