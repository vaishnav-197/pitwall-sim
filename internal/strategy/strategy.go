// Package strategy finds the fastest pit-stop plan with dynamic programming.
package strategy

import (
	"fmt"
	"math"
	"math/bits"
	"sort"
	"strings"

	"github.com/vaishnav-197/pitwall-sim/internal/tyre"
)

// Stint is a run of consecutive laps on one set of tyres.
type Stint struct {
	Compound string `json:"compound"`
	Laps     int    `json:"laps"`
}

// Plan is a full race strategy.
type Plan struct {
	Stints  []Stint `json:"stints"`
	Stops   int     `json:"stops"`
	PitLaps []int   `json:"pit_laps"` // you pit at the end of these laps
	Time    float64 `json:"time"`     // predicted race time in seconds
}

func (p Plan) String() string {
	parts := make([]string, len(p.Stints))
	for i, s := range p.Stints {
		parts[i] = fmt.Sprintf("%s×%d", s.Compound, s.Laps)
	}
	return strings.Join(parts, " → ")
}

// Optimize returns the fastest legal plan for each number of stops from 0 to
// Race.MaxStops (skipping stop counts that can't be legal).
//
// State after finishing a lap: (compound on car, age of that set,
// set of compounds used so far, stops made). From each state you either
// stay out or pit for any compound. Every state keeps only its fastest
// history, so the search is O(laps × states × compounds) instead of trying
// every combination of pit laps.
func Optimize(m *tyre.Model) ([]Plan, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	r := m.Race
	names := m.Names()
	comps := make([]tyre.Compound, len(names))
	for i, n := range names {
		comps[i] = m.Compounds[n]
	}

	nc, maxAge, nMask, nStops := len(names), r.Laps, 1<<len(names), r.MaxStops+1
	size := nc * (maxAge + 1) * nMask * nStops
	key := func(c, age, mask, s int) int { return ((c*(maxAge+1)+age)*nMask+mask)*nStops + s }

	inf := math.Inf(1)
	cur := make([]float64, size)
	for i := range cur {
		cur[i] = inf
	}
	// parent[lap][state] = previous state index; -1 for the first lap.
	parent := make([][]int32, r.Laps+1)

	parent[1] = make([]int32, size)
	for c := range comps {
		k := key(c, 1, 1<<c, 0)
		cur[k] = m.LapTime(comps[c], 0, 1)
		parent[1][k] = -1
	}

	for lap := 2; lap <= r.Laps; lap++ {
		next := make([]float64, size)
		for i := range next {
			next[i] = inf
		}
		par := make([]int32, size)
		relax := func(k int, t float64, from int) {
			if t < next[k] {
				next[k], par[k] = t, int32(from)
			}
		}
		for c := 0; c < nc; c++ {
			for age := 1; age < lap; age++ {
				for mask := 1; mask < nMask; mask++ {
					for s := 0; s < nStops; s++ {
						from := key(c, age, mask, s)
						t := cur[from]
						if t == inf {
							continue
						}
						// Stay out.
						relax(key(c, age+1, mask, s), t+m.LapTime(comps[c], age, lap), from)
						// Pit at the end of the previous lap for a fresh set.
						if s+1 < nStops {
							for c2 := 0; c2 < nc; c2++ {
								relax(key(c2, 1, mask|1<<c2, s+1),
									t+r.PitLoss+m.LapTime(comps[c2], 0, lap), from)
							}
						}
					}
				}
			}
		}
		cur, parent[lap] = next, par
	}

	// Best finishing state for each stop count.
	best := make([]int, nStops)
	for i := range best {
		best[i] = -1
	}
	for c := 0; c < nc; c++ {
		for age := 1; age <= maxAge; age++ {
			for mask := 1; mask < nMask; mask++ {
				if bits.OnesCount(uint(mask)) < r.MinCompounds {
					continue
				}
				for s := 0; s < nStops; s++ {
					k := key(c, age, mask, s)
					if cur[k] == inf {
						continue
					}
					if best[s] == -1 || cur[k] < cur[best[s]] {
						best[s] = k
					}
				}
			}
		}
	}

	decode := func(k int) (c, age int) {
		k /= nStops
		k /= nMask
		return k / (maxAge + 1), k % (maxAge + 1)
	}

	var plans []Plan
	for s, k := range best {
		if k == -1 {
			continue
		}
		// Walk parents back. A tyre age of 1 after lap 1 means a fresh set
		// went on, i.e. the car pitted at the end of the previous lap.
		comp := make([]int, r.Laps+1)
		fresh := make([]bool, r.Laps+1)
		state := k
		for lap := r.Laps; lap >= 1; lap-- {
			c, age := decode(state)
			comp[lap], fresh[lap] = c, age == 1
			state = int(parent[lap][state])
		}
		p := Plan{Time: cur[k]}
		for lap := 1; lap <= r.Laps; lap++ {
			if fresh[lap] {
				if lap > 1 {
					p.PitLaps = append(p.PitLaps, lap-1)
				}
				p.Stints = append(p.Stints, Stint{Compound: names[comp[lap]]})
			}
			p.Stints[len(p.Stints)-1].Laps++
		}
		p.Stops = len(p.Stints) - 1
		if p.Stops != s {
			return nil, fmt.Errorf("internal: decoded %d stops, expected %d", p.Stops, s)
		}
		plans = append(plans, p)
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].Time < plans[j].Time })
	return plans, nil
}

// RaceTime replays a plan lap by lap. Used to check the optimizer and by the
// simulator.
func RaceTime(m *tyre.Model, p Plan) (float64, error) {
	total, lap := 0.0, 0
	for i, s := range p.Stints {
		c, ok := m.Compounds[s.Compound]
		if !ok {
			return 0, fmt.Errorf("unknown compound %q", s.Compound)
		}
		if i > 0 {
			total += m.Race.PitLoss
		}
		for age := 0; age < s.Laps; age++ {
			lap++
			total += m.LapTime(c, age, lap)
		}
	}
	if lap != m.Race.Laps {
		return 0, fmt.Errorf("plan covers %d laps, race has %d", lap, m.Race.Laps)
	}
	return total, nil
}
