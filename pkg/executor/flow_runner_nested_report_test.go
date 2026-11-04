package executor

import (
	"context"
	"fmt"
	"testing"

	"github.com/devicelab-dev/maestro-runner/pkg/core"
	"github.com/devicelab-dev/maestro-runner/pkg/flow"
	"github.com/devicelab-dev/maestro-runner/pkg/report"
)

// shape is the report tree reduced to what nesting must preserve: each
// command's type, position and children.
type shape struct {
	ID       string
	Type     string
	Status   report.Status
	Children []shape
}

func shapeOf(cmds []report.Command) []shape {
	if len(cmds) == 0 {
		return nil
	}
	out := make([]shape, len(cmds))
	for i, c := range cmds {
		out[i] = shape{ID: c.ID, Type: c.Type, Status: c.Status, Children: shapeOf(c.SubCommands)}
	}
	return out
}

func sameShape(a, b []shape) bool {
	return fmt.Sprintf("%+v", a) == fmt.Sprintf("%+v", b)
}

func tapStep() flow.Step {
	return &flow.TapOnStep{BaseStep: flow.BaseStep{StepType: flow.StepTapOn}}
}

// Every compound step nested inside a runFlow keeps its own record in the
// report, carrying its children, at its position among its siblings.
func TestRunner_NestedCompoundStepsKeepRecordAndChildren(t *testing.T) {
	inner := func(kind string) flow.Step {
		switch kind {
		case "runFlow":
			return &flow.RunFlowStep{
				BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
				Steps:    []flow.Step{tapStep(), tapStep()},
			}
		case "repeat":
			return &flow.RepeatStep{
				BaseStep: flow.BaseStep{StepType: flow.StepRepeat},
				Times:    "2",
				Steps:    []flow.Step{tapStep()},
			}
		case "retry":
			return &flow.RetryStep{
				BaseStep: flow.BaseStep{StepType: flow.StepRetry},
				Steps:    []flow.Step{tapStep(), tapStep()},
			}
		}
		t.Fatalf("unknown compound kind %q", kind)
		return nil
	}
	pass := report.StatusPassed
	tap := func(id string) shape { return shape{ID: id, Type: "tapOn", Status: pass} }
	twoTaps := []shape{tap("sub-0"), tap("sub-1")}

	for _, kind := range []string{"runFlow", "repeat", "retry"} {
		t.Run(kind, func(t *testing.T) {
			// runFlow { tapOn, <kind> { ... }, tapOn }
			flows := []flow.Flow{{
				SourcePath: "nested.yaml",
				Config:     flow.Config{Name: "Nested " + kind},
				Steps: []flow.Step{
					&flow.RunFlowStep{
						BaseStep: flow.BaseStep{StepType: flow.StepRunFlow},
						Steps:    []flow.Step{tapStep(), inner(kind), tapStep()},
					},
				},
			}}

			outDir := t.TempDir()
			driver := &mockDriver{executeFunc: func(flow.Step) *core.CommandResult {
				return &core.CommandResult{Success: true}
			}}
			runner := New(driver, RunnerConfig{
				OutputDir: outDir,
				Artifacts: ArtifactNever,
				Device:    report.Device{ID: "test", Platform: "ios"},
			})
			if _, err := runner.Run(context.Background(), flows); err != nil {
				t.Fatalf("Run() error = %v", err)
			}

			_, details, err := report.ReadReport(outDir)
			if err != nil {
				t.Fatalf("ReadReport() error = %v", err)
			}
			if len(details) != 1 || len(details[0].Commands) != 1 {
				t.Fatalf("want 1 flow with 1 top-level command, got %+v", details)
			}

			got := shapeOf(details[0].Commands[0].SubCommands)
			want := []shape{
				tap("sub-0"),
				{ID: "sub-1", Type: kind, Status: pass, Children: twoTaps},
				tap("sub-2"),
			}
			if !sameShape(got, want) {
				t.Errorf("runFlow children:\n got  %+v\n want %+v", got, want)
			}
		})
	}
}

// Nesting composes: a runFlow inside a runFlow inside a runFlow keeps every
// level's record, and a failure deep inside is reported on each ancestor.
func TestRunner_DeeplyNestedRunFlowKeepsEveryLevel(t *testing.T) {
	calls := 0
	driver := &mockDriver{executeFunc: func(flow.Step) *core.CommandResult {
		calls++
		if calls == 2 {
			return &core.CommandResult{Success: false, Error: fmt.Errorf("boom")}
		}
		return &core.CommandResult{Success: true}
	}}
	runFlow := func(steps ...flow.Step) flow.Step {
		return &flow.RunFlowStep{BaseStep: flow.BaseStep{StepType: flow.StepRunFlow}, Steps: steps}
	}
	flows := []flow.Flow{{
		SourcePath: "deep.yaml",
		Config:     flow.Config{Name: "Deep"},
		// runFlow { runFlow { tapOn, runFlow { tapOn(fails) } } }
		Steps: []flow.Step{runFlow(runFlow(tapStep(), runFlow(tapStep())))},
	}}

	outDir := t.TempDir()
	runner := New(driver, RunnerConfig{
		OutputDir: outDir,
		Artifacts: ArtifactNever,
		Device:    report.Device{ID: "test", Platform: "ios"},
	})
	if _, err := runner.Run(context.Background(), flows); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	_, details, err := report.ReadReport(outDir)
	if err != nil {
		t.Fatalf("ReadReport() error = %v", err)
	}

	fail := report.StatusFailed
	got := shapeOf(details[0].Commands[0].SubCommands)
	want := []shape{{
		ID: "sub-0", Type: "runFlow", Status: fail, Children: []shape{
			{ID: "sub-0", Type: "tapOn", Status: report.StatusPassed},
			{ID: "sub-1", Type: "runFlow", Status: fail, Children: []shape{
				{ID: "sub-0", Type: "tapOn", Status: fail},
			}},
		},
	}}
	if !sameShape(got, want) {
		t.Errorf("runFlow children:\n got  %+v\n want %+v", got, want)
	}
	failed := details[0].Commands[0].SubCommands[0].SubCommands[1]
	if failed.Error == nil || failed.Error.Message == "" {
		t.Errorf("nested runFlow record lost its error: %+v", failed)
	}
}
