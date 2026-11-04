package flow

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// selectorFieldsInStepGo lists every "Type.Field" in step.go whose field holds a
// Selector or a Condition (which holds selectors), on a struct named *Step.
func selectorFieldsInStepGo(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "step.go", nil, 0)
	if err != nil {
		t.Fatalf("parse step.go: %v", err)
	}
	var out []string
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || !strings.HasSuffix(ts.Name.Name, "Step") {
				continue
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, f := range st.Fields.List {
				typ := f.Type
				if star, ok := typ.(*ast.StarExpr); ok {
					typ = star.X
				}
				id, ok := typ.(*ast.Ident)
				if !ok || (id.Name != "Selector" && id.Name != "Condition") {
					continue
				}
				for _, name := range f.Names {
					out = append(out, ts.Name.Name+"."+name.Name)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// Every selector a step carries must be in ExtractSelectors: variable
// expansion and the unsupported-selector warning both read only that list.
func TestExtractSelectors_CoversEverySelectorField(t *testing.T) {
	sel := func() *Selector { return &Selector{ID: "x"} }
	// Each case builds a step with that field set and returns the selectors
	// ExtractSelectors must return for it.
	covered := map[string]func() (Step, []*Selector){
		"TapOnStep.Selector": func() (Step, []*Selector) {
			s := &TapOnStep{}
			return s, []*Selector{&s.Selector}
		},
		"DoubleTapOnStep.Selector": func() (Step, []*Selector) {
			s := &DoubleTapOnStep{}
			return s, []*Selector{&s.Selector}
		},
		"LongPressOnStep.Selector": func() (Step, []*Selector) {
			s := &LongPressOnStep{}
			return s, []*Selector{&s.Selector}
		},
		"SwipeStep.Selector": func() (Step, []*Selector) {
			s := &SwipeStep{Selector: sel()}
			return s, []*Selector{s.Selector}
		},
		"SwipeStep.To": func() (Step, []*Selector) {
			s := &SwipeStep{Selector: sel(), To: sel()}
			return s, []*Selector{s.Selector, s.To}
		},
		"ScrollUntilVisibleStep.Element": func() (Step, []*Selector) {
			s := &ScrollUntilVisibleStep{}
			return s, []*Selector{&s.Element}
		},
		"InputTextStep.Selector": func() (Step, []*Selector) {
			s := &InputTextStep{}
			return s, []*Selector{&s.Selector}
		},
		"CopyTextFromStep.Selector": func() (Step, []*Selector) {
			s := &CopyTextFromStep{}
			return s, []*Selector{&s.Selector}
		},
		"AssertVisibleStep.Selector": func() (Step, []*Selector) {
			s := &AssertVisibleStep{}
			return s, []*Selector{&s.Selector}
		},
		"AssertNotVisibleStep.Selector": func() (Step, []*Selector) {
			s := &AssertNotVisibleStep{}
			return s, []*Selector{&s.Selector}
		},
		"AssertConditionStep.Condition": func() (Step, []*Selector) {
			s := &AssertConditionStep{Condition: Condition{Visible: sel(), NotVisible: sel()}}
			return s, []*Selector{s.Condition.Visible, s.Condition.NotVisible}
		},
		"WaitUntilStep.Visible": func() (Step, []*Selector) {
			s := &WaitUntilStep{Visible: sel()}
			return s, []*Selector{s.Visible}
		},
		"WaitUntilStep.NotVisible": func() (Step, []*Selector) {
			s := &WaitUntilStep{NotVisible: sel()}
			return s, []*Selector{s.NotVisible}
		},
		"RunFlowStep.When": func() (Step, []*Selector) {
			s := &RunFlowStep{When: &Condition{Visible: sel(), NotVisible: sel()}}
			return s, []*Selector{s.When.Visible, s.When.NotVisible}
		},
		"UploadFileStep.Selector": func() (Step, []*Selector) {
			s := &UploadFileStep{}
			return s, []*Selector{&s.Selector}
		},
		"TakeScreenshotStep.CropOn": func() (Step, []*Selector) {
			s := &TakeScreenshotStep{CropOn: sel()}
			return s, []*Selector{s.CropOn}
		},
	}
	exempt := map[string]string{
		"SwipeStep.From":   "the parser folds it into SwipeStep.Selector",
		"RepeatStep.While": "executeRepeat expands it at every check, since each iteration may change it",
	}

	for _, field := range selectorFieldsInStepGo(t) {
		build, ok := covered[field]
		if !ok {
			if _, ok := exempt[field]; !ok {
				t.Errorf("%s holds a selector but is neither listed by ExtractSelectors nor exempt", field)
			}
			continue
		}
		step, want := build()
		got := ExtractSelectors(step)
		for _, w := range want {
			found := false
			for _, g := range got {
				if g == w {
					found = true
				}
			}
			if !found {
				t.Errorf("%s: ExtractSelectors(%T) misses it", field, step)
			}
		}
	}
	for field := range covered {
		if !strings.Contains(strings.Join(selectorFieldsInStepGo(t), " "), field) {
			t.Errorf("covered entry %s no longer exists in step.go", field)
		}
	}
}
