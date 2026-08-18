package evidence

import "testing"

func drainRunwayShadow(r *runwayShadow, sample OutcomeSample) int {
	for round := 1; round <= 100; round++ {
		if r.observe(sample).spent {
			return round
		}
	}
	return -1
}

func TestRunwayShadowPricesOutcomesWithoutAFixedRoundCliff(t *testing.T) {
	var repeating runwayShadow
	if got := drainRunwayShadow(&repeating, OutcomeSample{}); got != runwayStartBalance/runwayRoundCost {
		t.Errorf("empty rounds lasted %d, want %d", got, runwayStartBalance/runwayRoundCost)
	}

	var exploring runwayShadow
	if got := drainRunwayShadow(&exploring, OutcomeSample{Exploration: 1}); got != runwayStartBalance {
		t.Errorf("exploration rounds lasted %d, want %d", got, runwayStartBalance)
	}

	for name, sample := range map[string]OutcomeSample{
		"changing": {Churn: 1},
		"checking": {Discriminating: 1},
	} {
		var productive runwayShadow
		if got := drainRunwayShadow(&productive, sample); got != -1 {
			t.Errorf("%s account spent after %d rounds", name, got)
		}
	}
}

func TestRunwayShadowIsBoundedAndSeparatesDryFromIdle(t *testing.T) {
	var r runwayShadow
	for range 100 {
		r.observe(OutcomeSample{Discriminating: 1})
	}
	if got := drainRunwayShadow(&r, OutcomeSample{Exploration: 1}); got != runwayMaxBalance {
		t.Errorf("banked account lasted %d exploration rounds, want %d", got, runwayMaxBalance)
	}

	var counts runwayShadow
	for range 3 {
		counts.observe(OutcomeSample{Exploration: 1})
	}
	state := counts.observe(OutcomeSample{})
	if state.idle != 4 || state.dry != 1 {
		t.Fatalf("quiet round state = %+v, want idle 4 dry 1", state)
	}
	state = counts.observe(OutcomeSample{Churn: 1})
	if state.idle != 0 || state.dry != 0 {
		t.Fatalf("change state = %+v, want cleared counters", state)
	}
}
