package flow

import (
	"reflect"
	"testing"
)

// A clone shares no reference with its original: rewriting everything a
// clone points at leaves the original exactly as it was.
func TestCloneStep_SharesNothing(t *testing.T) {
	build := func() *LaunchAppStep {
		return &LaunchAppStep{
			BaseStep:    BaseStep{StepType: StepLaunchApp, StepLabel: "launch"},
			AppID:       "${APP}",
			Arguments:   map[string]any{"isE2E": "${E2E}", "nested": []any{"x"}},
			Environment: map[string]string{"K": "${V}"},
		}
	}
	original := build()
	clone := CloneStep(original).(*LaunchAppStep)

	clone.AppID = "changed"
	clone.Arguments["isE2E"] = "changed"
	clone.Arguments["nested"].([]any)[0] = "changed"
	clone.Environment["K"] = "changed"

	if !reflect.DeepEqual(original, build()) {
		t.Errorf("original changed through its clone: %+v", original)
	}
}

func TestCloneStep_DeepCopiesSelectorsAndConditions(t *testing.T) {
	build := func() *RunFlowStep {
		return &RunFlowStep{
			When:  &Condition{Visible: &Selector{ID: "a", ChildOf: &Selector{ID: "p"}}},
			Steps: []Step{&TapOnStep{Selector: Selector{ID: "t"}}},
			Env:   map[string]string{"X": "1"},
		}
	}
	original := build()
	clone := CloneStep(original).(*RunFlowStep)

	clone.When.Visible.ID = "changed"
	clone.When.Visible.ChildOf.ID = "changed"
	clone.Steps[0].(*TapOnStep).Selector.ID = "changed"
	clone.Env["X"] = "changed"

	if !reflect.DeepEqual(original, build()) {
		t.Errorf("original changed through its clone: when=%+v", original.When.Visible)
	}
}

func TestCloneStep_Nil(t *testing.T) {
	if CloneStep(nil) != nil {
		t.Error("CloneStep(nil) != nil")
	}
}

// Nil pointers, nil maps and nil interfaces inside a slice clone as nil.
func TestCloneStep_KeepsNils(t *testing.T) {
	original := &RunFlowStep{Steps: []Step{nil, &TapOnStep{}}}
	clone := CloneStep(original).(*RunFlowStep)
	if clone.When != nil || clone.Env != nil || clone.Steps[0] != nil {
		t.Errorf("clone = %+v, want nil When, Env and Steps[0]", clone)
	}
	if !reflect.DeepEqual(clone, original) {
		t.Errorf("clone = %+v, want %+v", clone, original)
	}
}
