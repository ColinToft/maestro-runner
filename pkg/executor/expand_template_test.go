package executor

import (
	"context"
	"fmt"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/report"
)

// Each iteration of a repeat expands its steps afresh: a selector built from a
// variable that changes per iteration reaches the driver with that
// iteration's value, not the first one's.
func TestRunner_Repeat_ExpandsEachIterationAfresh(t *testing.T) {
	var tapped []string
	driver := &mockDriver{
		executeFunc: func(step flow.Step) *core.CommandResult {
			if tap, ok := step.(*flow.TapOnStep); ok {
				tapped = append(tapped, tap.Selector.ID)
			}
			return &core.CommandResult{Success: true}
		},
	}
	runner := New(driver, RunnerConfig{
		OutputDir: t.TempDir(),
		Artifacts: ArtifactNever,
		Device:    report.Device{ID: "test", Platform: "android"},
	})
	flows := []flow.Flow{{
		SourcePath: "test.yaml",
		Config:     flow.Config{Name: "Repeat expansion"},
		Steps: []flow.Step{
			&flow.EvalScriptStep{BaseStep: flow.BaseStep{StepType: flow.StepEvalScript}, Script: "${output.i = 0}"},
			&flow.RepeatStep{
				BaseStep: flow.BaseStep{StepType: flow.StepRepeat},
				Times:    "3",
				Steps: []flow.Step{
					&flow.EvalScriptStep{BaseStep: flow.BaseStep{StepType: flow.StepEvalScript}, Script: "${output.i = output.i + 1}"},
					&flow.TapOnStep{
						BaseStep: flow.BaseStep{StepType: flow.StepTapOn},
						Selector: flow.Selector{ID: "row-${output.i}"},
					},
				},
			},
		},
	}}

	result, err := runner.Run(context.Background(), flows)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != report.StatusPassed {
		t.Fatalf("Status = %v, want %v", result.Status, report.StatusPassed)
	}
	want := []string{"row-1", "row-2", "row-3"}
	if len(tapped) != len(want) {
		t.Fatalf("tapped = %v, want %v", tapped, want)
	}
	for i := range want {
		if tapped[i] != want[i] {
			t.Errorf("tapped = %v, want %v", tapped, want)
			break
		}
	}
}

// Expand never writes into the step it is given, so expanding one template
// twice under different variables gives two different steps.
func TestScriptEngine_Expand_LeavesTheTemplate(t *testing.T) {
	se := NewScriptEngine()
	defer se.Close()

	template := &flow.SwipeStep{
		Selector: &flow.Selector{ID: "${FROM}"},
		To:       &flow.Selector{ID: "${TO}", ChildOf: &flow.Selector{ID: "day-${TO}"}},
		Start:    "${S}",
	}

	se.SetVariable("FROM", "a")
	se.SetVariable("TO", "b")
	se.SetVariable("S", "1%, 1%")
	first := se.Expand(template).(*flow.SwipeStep)
	se.SetVariable("TO", "c")
	second := se.Expand(template).(*flow.SwipeStep)

	if template.To.ID != "${TO}" || template.To.ChildOf.ID != "day-${TO}" || template.Selector.ID != "${FROM}" || template.Start != "${S}" {
		t.Errorf("template was rewritten: %+v to=%+v", template, template.To)
	}
	if first.To.ID != "b" || second.To.ID != "c" {
		t.Errorf("to = %q then %q, want %q then %q", first.To.ID, second.To.ID, "b", "c")
	}
	if first.To.ChildOf.ID != "day-b" || second.To.ChildOf.ID != "day-c" {
		t.Errorf("to.childOf = %q then %q, want %q then %q", first.To.ChildOf.ID, second.To.ChildOf.ID, "day-b", "day-c")
	}
}

// A repeat's while condition is expanded at every check, so it sees the
// variables its own iterations change.
func TestRunner_RepeatWhile_ExpandsAtEveryCheck(t *testing.T) {
	var checked []string
	taps := 0
	driver := &mockDriver{
		executeFunc: func(step flow.Step) *core.CommandResult {
			switch s := step.(type) {
			case *flow.AssertVisibleStep:
				checked = append(checked, s.Selector.ID)
				return &core.CommandResult{Success: s.Selector.ID != "row-3"}
			case *flow.TapOnStep:
				taps++
			}
			return &core.CommandResult{Success: true}
		},
	}
	runner := New(driver, RunnerConfig{
		OutputDir: t.TempDir(),
		Artifacts: ArtifactNever,
		Device:    report.Device{ID: "test", Platform: "android"},
	})
	flows := []flow.Flow{{
		SourcePath: "test.yaml",
		Config:     flow.Config{Name: "Repeat while expansion"},
		Steps: []flow.Step{
			&flow.EvalScriptStep{BaseStep: flow.BaseStep{StepType: flow.StepEvalScript}, Script: "${output.i = 1}"},
			&flow.RepeatStep{
				BaseStep: flow.BaseStep{StepType: flow.StepRepeat},
				Times:    "5",
				While:    flow.Condition{Visible: &flow.Selector{ID: "row-${output.i}"}},
				Steps: []flow.Step{
					&flow.TapOnStep{BaseStep: flow.BaseStep{StepType: flow.StepTapOn}, Selector: flow.Selector{ID: "x"}},
					&flow.EvalScriptStep{BaseStep: flow.BaseStep{StepType: flow.StepEvalScript}, Script: "${output.i = output.i + 1}"},
				},
			},
		},
	}}

	result, err := runner.Run(context.Background(), flows)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != report.StatusPassed {
		t.Fatalf("Status = %v, want %v", result.Status, report.StatusPassed)
	}
	if want := "[row-1 row-2 row-3]"; fmt.Sprint(checked) != want || taps != 2 {
		t.Errorf("checked = %v, taps = %d; want %s, 2", checked, taps, want)
	}
}
