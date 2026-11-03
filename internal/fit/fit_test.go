package fit

import (
	"math"
	"math/rand"
	"strings"
	"testing"

	"github.com/vaishnav-197/pitwall-sim/internal/tyre"
)

var truth = &tyre.Model{
	Race: tyre.Race{Laps: 50, FuelPerLap: 0.04},
	Compounds: map[string]tyre.Compound{
		"soft":   {Base: 90.5, Lin: 0.09, Quad: 0.004},
		"medium": {Base: 91.0, Lin: 0.06, Quad: 0.002},
		"hard":   {Base: 91.6, Lin: 0.03, Quad: 0.001},
	},
}

func practice(noise float64, seed int64) []Lap {
	rng := rand.New(rand.NewSource(seed))
	var laps []Lap
	for _, name := range truth.Names() {
		c := truth.Compounds[name]
		for _, start := range []int{1, 15, 30} {
			for age := 0; age < 15; age++ {
				lap := start + age
				laps = append(laps, Lap{name, age, lap, truth.LapTime(c, age, lap) + rng.NormFloat64()*noise})
			}
		}
	}
	return laps
}

func TestFitRecoversExactParametersWithoutNoise(t *testing.T) {
	res, err := Fit(practice(0, 1), truth.Race.Laps)
	if err != nil {
		t.Fatal(err)
	}
	near := func(name string, got, want float64) {
		if math.Abs(got-want) > 1e-6 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}
	near("fuel", res.FuelPerLap, 0.04)
	for name, want := range truth.Compounds {
		got := res.Compounds[name]
		near(name+".base", got.Base, want.Base)
		near(name+".lin", got.Lin, want.Lin)
		near(name+".quad", got.Quad, want.Quad)
	}
	if res.R2 < 0.999999 {
		t.Errorf("R2 = %v", res.R2)
	}
}

// With noise the coefficients wobble, but predicted stint times should stay
// close to the truth, which is what the optimizer actually uses.
func TestFitWithNoisePredictsStintTimes(t *testing.T) {
	res, err := Fit(practice(0.2, 3), truth.Race.Laps)
	if err != nil {
		t.Fatal(err)
	}
	fitted := &tyre.Model{Race: truth.Race, Compounds: res.Compounds}
	fitted.Race.FuelPerLap = res.FuelPerLap
	for name := range truth.Compounds {
		var a, b float64
		for age := 0; age < 15; age++ {
			a += truth.LapTime(truth.Compounds[name], age, 20+age)
			b += fitted.LapTime(fitted.Compounds[name], age, 20+age)
		}
		if math.Abs(a-b) > 1.0 {
			t.Errorf("%s: 15-lap stint off by %.2fs", name, a-b)
		}
	}
	if res.RMSE > 0.3 {
		t.Errorf("RMSE = %v, want close to the 0.2 noise", res.RMSE)
	}
}

func TestFitNeedsVariedTyreAges(t *testing.T) {
	var laps []Lap
	for lap := 1; lap <= 20; lap++ {
		laps = append(laps, Lap{"soft", 0, lap, 90}) // every lap on a fresh set
	}
	if _, err := Fit(laps, 50); err == nil {
		t.Fatal("want singular-system error")
	}
}

func TestReadCSV(t *testing.T) {
	laps, err := ReadCSV(strings.NewReader("compound,tyre_age,lap,lap_time\nsoft,0,1,90.5\nhard, 3 ,4,91.2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(laps) != 2 || laps[1] != (Lap{"hard", 3, 4, 91.2}) {
		t.Fatalf("got %+v", laps)
	}
	for _, bad := range []string{
		"a,b,c,d\nsoft,0,1,90\n",
		"compound,tyre_age,lap,lap_time\nsoft,x,1,90\n",
	} {
		if _, err := ReadCSV(strings.NewReader(bad)); err == nil {
			t.Errorf("want error for %q", bad)
		}
	}
}
