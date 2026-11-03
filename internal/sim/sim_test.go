package sim

import (
	"math"
	"math/rand"
	"testing"

	"github.com/vaishnav-197/pitwall-sim/internal/strategy"
	"github.com/vaishnav-197/pitwall-sim/internal/tyre"
)

func race() (*tyre.Model, []strategy.Plan) {
	m := &tyre.Model{
		Race: tyre.Race{Laps: 30, FuelPerLap: 0.03, PitLoss: 20, MinCompounds: 2, MaxStops: 2},
		Compounds: map[string]tyre.Compound{
			"soft": {Base: 90, Lin: 0.1, Quad: 0.004},
			"hard": {Base: 91, Lin: 0.04, Quad: 0.001},
		},
	}
	plans, err := strategy.Optimize(m)
	if err != nil {
		panic(err)
	}
	return m, plans
}

func TestNoSafetyCarNoNoiseMatchesOptimizer(t *testing.T) {
	m, plans := race()
	cfg := DefaultConfig()
	cfg.SCProbPerLap, cfg.LapNoise, cfg.Runs = 0, 0, 10
	stats, err := Run(m, plans, cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range stats {
		if math.Abs(s.Mean-s.Plan.Time) > 1e-6 || s.Std > 1e-9 {
			t.Errorf("%s: mean %.4f std %.4f, want %.4f exactly", s.Plan, s.Mean, s.Std, s.Plan.Time)
		}
	}
	if stats[0].WinShare != 1 {
		t.Errorf("fastest plan should win every clean race, got %v", stats[0].WinShare)
	}
}

func TestSeedIsReproducible(t *testing.T) {
	m, plans := race()
	cfg := DefaultConfig()
	cfg.Runs = 500
	a, _ := Run(m, plans, cfg)
	b, _ := Run(m, plans, cfg)
	for i := range a {
		if a[i].Mean != b[i].Mean || a[i].WinShare != b[i].WinShare {
			t.Fatal("same seed gave different results")
		}
	}
}

// A safety car on laps 10-12 with a stop planned for lap 14: reacting means
// pitting at the end of lap 10 for half the pit loss.
func TestReactingToSafetyCarSavesPitTime(t *testing.T) {
	m := &tyre.Model{
		Race:      tyre.Race{Laps: 20, PitLoss: 20},
		Compounds: map[string]tyre.Compound{"soft": {Base: 90}, "hard": {Base: 90}},
	}
	plan := strategy.Plan{Stints: []strategy.Stint{{Compound: "soft", Laps: 14}, {Compound: "hard", Laps: 6}}, PitLaps: []int{14}, Stops: 1}
	sc := make([]bool, 21)
	sc[10], sc[11], sc[12] = true, true, true

	cfg := DefaultConfig()
	cfg.LapNoise = 0
	static := raceTime(m, plan, sc, cfg, rand.New(rand.NewSource(1)))
	cfg.ReactWindow = 5
	react := raceTime(m, plan, sc, cfg, rand.New(rand.NewSource(1)))

	if math.Abs((static-react)-10) > 1e-9 { // 20s pit loss vs 10s under SC
		t.Fatalf("static %.2f react %.2f, want react 10s faster", static, react)
	}
}

func TestConfigValidation(t *testing.T) {
	m, plans := race()
	bad := []func(*Config){
		func(c *Config) { c.Runs = 0 },
		func(c *Config) { c.SCProbPerLap = 2 },
		func(c *Config) { c.SCMaxLaps = 1; c.SCMinLaps = 3 },
		func(c *Config) { c.SCLapFactor = 0.5 },
		func(c *Config) { c.ReactWindow = -1 },
	}
	for i, f := range bad {
		cfg := DefaultConfig()
		f(&cfg)
		if _, err := Run(m, plans, cfg); err == nil {
			t.Errorf("case %d: want error", i)
		}
	}
	if _, err := Run(m, nil, DefaultConfig()); err == nil {
		t.Error("want error for no plans")
	}
}
