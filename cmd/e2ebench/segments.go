package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// segment is one leg of a task: leg 1 starts the session, later legs resume it.
// Segmenting reaches the states that only appear in a long session — reload,
// prefix reconstruction, compaction across a boundary — without waiting hours
// for them to arrive on their own.
type segment struct {
	index    int
	maxSteps int
	prompt   string
	resume   bool
}

// continuationPrompt is deliberately empty of new instruction: a resumed leg
// must exercise the session's own memory of the task, not be handed the task
// again. A prompt that restates the work would hide exactly the degradation
// this measures.
const continuationPrompt = "Continue from where you left off."

// planSegments splits a task into count legs, dividing its step budget and
// giving the remainder to the last leg so the total never exceeds max_steps.
// A steer entry replaces a leg's continuation prompt.
func planSegments(t task, count int, steers map[int]string) []segment {
	if count < 2 {
		return []segment{{index: 1, maxSteps: t.MaxSteps, prompt: t.Prompt}}
	}
	per := t.MaxSteps / count
	out := make([]segment, 0, count)
	for i := 1; i <= count; i++ {
		steps := per
		if i == count {
			steps = t.MaxSteps - per*(count-1)
		}
		seg := segment{index: i, maxSteps: steps, prompt: continuationPrompt, resume: true}
		if i == 1 {
			seg.prompt, seg.resume = t.Prompt, false
		}
		if steer, ok := steers[i]; ok {
			seg.prompt = steer
		}
		out = append(out, seg)
	}
	return out
}

// segmentSettings validates the two LongRun pressure flags together: a steer
// aimed past the last leg would never be delivered, and silently dropping a
// user turn is exactly the kind of quiet no-op a benchmark must not have.
func segmentSettings(segments int, spec string) map[int]string {
	steers, err := parseSteers(spec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "steer:", err)
		os.Exit(2)
	}
	for leg := range steers {
		if leg > segments {
			fmt.Fprintf(os.Stderr, "steer: leg %d needs -segments %d or more; the run has %d\n", leg, leg, segments)
			os.Exit(2)
		}
	}
	return steers
}

// parseSteers reads "message@2,another@3": deliver that message as leg N's
// prompt. Steering is a user turn arriving mid-task, so it belongs to a leg
// boundary rather than to a wall-clock instant nothing can reproduce.
func parseSteers(spec string) (map[int]string, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	out := map[int]string{}
	for field := range strings.SplitSeq(spec, ",") {
		message, at, ok := strings.Cut(strings.TrimSpace(field), "@")
		if !ok {
			return nil, fmt.Errorf("steer %q: want <message>@<segment>", field)
		}
		leg, err := strconv.Atoi(strings.TrimSpace(at))
		if err != nil || leg < 2 {
			return nil, fmt.Errorf("steer %q: the segment must be 2 or later — leg 1 carries the task itself", field)
		}
		if strings.TrimSpace(message) == "" {
			return nil, fmt.Errorf("steer %q: the message is empty", field)
		}
		out[leg] = strings.TrimSpace(message)
	}
	return out, nil
}

// pressureFlags groups the LongRun knobs: neutral metering, injected faults,
// and segmented resume with steering. They are registered and validated
// together because they only make sense together.
type pressureFlags struct {
	meter, faults, steer *string
	segments             *int
}

func registerPressureFlags() pressureFlags {
	return pressureFlags{
		meter:    flag.String("meter", "", "suite mode: route the benchmarked provider through the neutral measuring proxy, using this config.toml as the source (e.g. ~/.reasonix/config.toml). Spend is then counted at the request boundary instead of trusted from the harness"),
		faults:   flag.String("faults", "", "suite mode: inject provider failures through the meter — absolute indices (3:429) and/or a cadence that scales with the run (every:5:500). Requires -meter; the report gains a fault-recovery readout"),
		steer:    flag.String("steer", "", "suite mode: deliver a user turn at a leg boundary, e.g. \"also handle empty input@2\" (requires -segments >= that leg)"),
		segments: flag.Int("segments", 1, "suite mode: split each task into N resumed legs (--continue between them), reaching reload and compaction pressure without waiting hours for it. The step budget is divided, never multiplied"),
	}
}

func (p pressureFlags) settings() (config string, faults faultScript, segments int, steers map[int]string) {
	config, faults = meterSettings(*p.meter, *p.faults)
	return config, faults, *p.segments, segmentSettings(*p.segments, *p.steer)
}
