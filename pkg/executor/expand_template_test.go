package executor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// runSteps runs one flow with the given config and steps, failing the test
// unless it passes.
func runSteps(t *testing.T, cfg flow.Config, env map[string]string, execute func(flow.Step) *core.CommandResult, steps ...flow.Step) {
	t.Helper()
	runner := New(&mockDriver{executeFunc: execute}, RunnerConfig{
		OutputDir: t.TempDir(),
		Artifacts: ArtifactNever,
		Device:    report.Device{ID: "test", Platform: "android"},
		Env:       env,
	})
	result, err := runner.Run(context.Background(), []flow.Flow{{SourcePath: filepath.Join(t.TempDir(), "test.yaml"), Config: cfg, Steps: steps}})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.Status != report.StatusPassed {
		t.Fatalf("Status = %v, want %v", result.Status, report.StatusPassed)
	}
}

func evalScript(script string) *flow.EvalScriptStep {
	return &flow.EvalScriptStep{BaseStep: flow.BaseStep{StepType: flow.StepEvalScript}, Script: script}
}

func tapID(id string) *flow.TapOnStep {
	return &flow.TapOnStep{BaseStep: flow.BaseStep{StepType: flow.StepTapOn}, Selector: flow.Selector{ID: id}}
}

// recordTaps returns a driver that records each tap's selector ID in *ids.
func recordTaps(ids *[]string) func(flow.Step) *core.CommandResult {
	return func(step flow.Step) *core.CommandResult {
		if tap, ok := step.(*flow.TapOnStep); ok {
			*ids = append(*ids, tap.Selector.ID)
		}
		return &core.CommandResult{Success: true}
	}
}

// Every way a step is run again within one execution expands it afresh: each
// retry attempt, and each pass through an inline runFlow inside a repeat.
func TestRunner_ReusedSteps_ExpandEachRun(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		var tapped []string
		attempt := func(step flow.Step) *core.CommandResult {
			if tap, ok := step.(*flow.TapOnStep); ok {
				tapped = append(tapped, tap.Selector.ID)
				return &core.CommandResult{Success: tap.Selector.ID == "row-2", Error: fmt.Errorf("not yet")}
			}
			return &core.CommandResult{Success: true}
		}
		runSteps(t, flow.Config{Name: "retry"}, nil, attempt,
			evalScript("${output.i = 0}"),
			&flow.RetryStep{
				BaseStep:   flow.BaseStep{StepType: flow.StepRetry},
				MaxRetries: "3",
				Steps:      []flow.Step{evalScript("${output.i = output.i + 1}"), tapID("row-${output.i}")},
			})
		if fmt.Sprint(tapped) != "[row-1 row-2]" {
			t.Errorf("tapped = %v, want [row-1 row-2]", tapped)
		}
	})
	t.Run("inline runFlow in a repeat", func(t *testing.T) {
		var tapped []string
		runSteps(t, flow.Config{Name: "inline"}, nil, recordTaps(&tapped),
			evalScript("${output.i = 0}"),
			&flow.RepeatStep{
				BaseStep: flow.BaseStep{StepType: flow.StepRepeat},
				Times:    "2",
				Steps: []flow.Step{&flow.RunFlowStep{
					BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
					Steps:    []flow.Step{evalScript("${output.i = output.i + 1}"), tapID("row-${output.i}")},
				}},
			})
		if fmt.Sprint(tapped) != "[row-1 row-2]" {
			t.Errorf("tapped = %v, want [row-1 row-2]", tapped)
		}
	})
}

// A lifecycle step naming no app gets the innermost flow's appId, expanded,
// on its own copy: the parsed step keeps naming none.
func TestRunner_DefaultAppID_IsTheInnermostFlows(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub.yaml")
	if err := os.WriteFile(sub, []byte("appId: com.sub\n---\n- launchApp\n- repeat:\n    times: 1\n    commands:\n      - stopApp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var apps []string
	record := func(step flow.Step) *core.CommandResult {
		switch s := step.(type) {
		case *flow.LaunchAppStep:
			apps = append(apps, "launch "+s.AppID)
		case *flow.StopAppStep:
			apps = append(apps, "stop "+s.AppID)
		case *flow.ClearStateStep:
			apps = append(apps, "clear "+s.AppID)
		}
		return &core.CommandResult{Success: true}
	}
	launch := &flow.LaunchAppStep{BaseStep: flow.BaseStep{StepType: flow.StepLaunchApp}}
	runSteps(t, flow.Config{Name: "appId", AppID: "${APP_ID}"}, map[string]string{"APP_ID": "com.cli"}, record,
		&flow.RepeatStep{BaseStep: flow.BaseStep{StepType: flow.StepRepeat}, Times: "2", Steps: []flow.Step{launch}},
		&flow.RunFlowStep{BaseStep: flow.BaseStep{StepType: flow.StepRunFlow}, File: sub},
		&flow.ClearStateStep{BaseStep: flow.BaseStep{StepType: flow.StepClearState}},
	)
	want := "[launch com.cli launch com.cli launch com.sub stop com.sub clear com.cli]"
	if fmt.Sprint(apps) != want {
		t.Errorf("apps = %v, want %s", apps, want)
	}
	if launch.AppID != "" {
		t.Errorf("parsed launchApp now names %q; it must stay a template", launch.AppID)
	}
}

// A condition is expanded where it is evaluated, through whichever path
// evaluates it: every selector, relative ones included, and its platform.
func TestScriptEngine_Conditions_ExpandedWhereEvaluated(t *testing.T) {
	cond := flow.Condition{
		Visible:    &flow.Selector{ID: "${V}", ChildOf: &flow.Selector{ID: "${P}"}},
		NotVisible: &flow.Selector{Text: "${N}"},
		Platform:   "${PLATFORM}",
	}
	evaluators := map[string]func(se *ScriptEngine, driver core.Driver) bool{
		"CheckCondition": func(se *ScriptEngine, driver core.Driver) bool {
			return se.CheckCondition(context.Background(), cond, driver)
		},
		"ExecuteAssertCondition": func(se *ScriptEngine, driver core.Driver) bool {
			return se.ExecuteAssertCondition(context.Background(), &flow.AssertConditionStep{Condition: cond}, driver).Success
		},
	}
	for name, evaluate := range evaluators {
		for _, platform := range []string{"android", "ios"} {
			t.Run(name+"/"+platform, func(t *testing.T) {
				se := NewScriptEngine()
				defer se.Close()
				for k, v := range map[string]string{"V": "row", "P": "day", "N": "gone", "PLATFORM": platform} {
					se.SetVariable(k, v)
				}
				var seen []string
				driver := &mockDriver{executeFunc: func(step flow.Step) *core.CommandResult {
					switch s := step.(type) {
					case *flow.AssertVisibleStep:
						seen = append(seen, s.Selector.ID+" in "+s.Selector.ChildOf.ID)
					case *flow.AssertNotVisibleStep:
						seen = append(seen, "not "+s.Selector.Text)
					}
					return &core.CommandResult{Success: true}
				}}

				passed := evaluate(se, driver)

				// The mock device is android: on ios the platform gate ends
				// the evaluation before any selector is checked.
				wantSeen := "[row in day not gone]"
				if platform == "ios" {
					wantSeen = "[]"
				}
				if fmt.Sprint(seen) != wantSeen {
					t.Errorf("checked %v, want %s", seen, wantSeen)
				}
				// An assertCondition on another platform is skipped, not failed.
				wantPassed := platform == "android" || name == "ExecuteAssertCondition"
				if passed != wantPassed {
					t.Errorf("passed = %v, want %v", passed, wantPassed)
				}
				if cond.Visible.ID != "${V}" || cond.Visible.ChildOf.ID != "${P}" || cond.Platform != "${PLATFORM}" {
					t.Errorf("condition was rewritten: %+v", cond)
				}
			})
		}
	}
}
