package strategy

import (
	"math"
	"testing"

	"github.com/vaishnav-197/pitwall-sim/internal/tyre"
)

func smallRace() *tyre.Model {
	return &tyre.Model{
		Race: tyre.Race{Laps: 14, FuelPerLap: 0.03, PitLoss: 4, MinCompounds: 2, MaxStops: 3},
		Compounds: map[string]tyre.Compound{
			"soft":   {Base: 90.0, Lin: 0.20, Quad: 0.02, CliffAge: 5, CliffRate: 0.8},
			"medium": {Base: 90.6, Lin: 0.10, Quad: 0.01},
			"hard":   {Base: 91.1, Lin: 0.05, Quad: 0.002},
		},
	}
}

// bruteForce tries every way to split the race into stints with every
// compound order and returns the best legal time per stop count.
func bruteForce(m *tyre.Model) map[int]float64 {
	names := m.Names()
	best := map[int]float64{}
	var rec func(stints []Stint, lapsLeft int)
	rec = func(stints []Stint, lapsLeft int) {
		if lapsLeft == 0 {
			used := map[string]bool{}
			for _, s := range stints {
				used[s.Compound] = true
			}
			if len(used) < m.Race.MinCompounds {
				return
			}
			t, err := RaceTime(m, Plan{Stints: stints})
			if err != nil {
				panic(err)
			}
			stops := len(stints) - 1
			if old, ok := best[stops]; !ok || t < old {
				best[stops] = t
			}
			return
		}
		if len(stints) > m.Race.MaxStops {
			return
		}
		for n := 1; n <= lapsLeft; n++ {
			for _, c := range names {
				rec(append(append([]Stint(nil), stints...), Stint{c, n}), lapsLeft-n)
			}
		}
	}
	rec(nil, m.Race.Laps)
	return best
}

func TestOptimizeMatchesBruteForce(t *testing.T) {
	m := smallRace()
	plans, err := Optimize(m)
	if err != nil {
		t.Fatal(err)
	}
	want := bruteForce(m)
	if len(plans) != len(want) {
		t.Fatalf("got %d plans, brute force found %d stop counts", len(plans), len(want))
	}
	for _, p := range plans {
		if math.Abs(p.Time-want[p.Stops]) > 1e-9 {
			t.Errorf("%d-stop: optimizer %.4f, brute force %.4f", p.Stops, p.Time, want[p.Stops])
		}
		replay, err := RaceTime(m, p)
		if err != nil {
			t.Fatal(err)
		}
		if math.Abs(replay-p.Time) > 1e-9 {
			t.Errorf("%d-stop: plan says %.4f, replay gives %.4f", p.Stops, p.Time, replay)
		}
		if len(p.PitLaps) != p.Stops {
			t.Errorf("%d-stop has pit laps %v", p.Stops, p.PitLaps)
		}
	}
	for i := 1; i < len(plans); i++ {
		if plans[i].Time < plans[i-1].Time {
			t.Fatal("plans not sorted fastest first")
		}
	}
}

func TestTwoCompoundRuleRemovesNoStopPlan(t *testing.T) {
	plans, err := Optimize(smallRace())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range plans {
		if p.Stops == 0 {
			t.Fatal("0-stop plan can't use two compounds")
		}
	}
}

// With one compound and a brutal cliff, the best plan pits for the same
// compound. That stop must still show up as two stints.
func TestSameCompoundStopIsDecoded(t *testing.T) {
	m := &tyre.Model{
		Race:      tyre.Race{Laps: 10, PitLoss: 1, MinCompounds: 1, MaxStops: 1},
		Compounds: map[string]tyre.Compound{"soft": {Base: 90, CliffAge: 4, CliffRate: 5}},
	}
	plans, err := Optimize(m)
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].Stops != 1 || len(plans[0].Stints) != 2 {
		t.Fatalf("best plan = %+v, want a soft→soft one-stop", plans[0])
	}
	if plans[0].String() != "soft×5 → soft×5" {
		t.Fatalf("got %s", plans[0])
	}
}

func TestRaceTimeRejectsWrongLength(t *testing.T) {
	if _, err := RaceTime(smallRace(), Plan{Stints: []Stint{{"soft", 3}}}); err == nil {
		t.Fatal("want error")
	}
	if _, err := RaceTime(smallRace(), Plan{Stints: []Stint{{"wet", 14}}}); err == nil {
		t.Fatal("want error for unknown compound")
	}
}
