package cdp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devicelab-dev/maestro-runner/pkg/flow"
)

// actionablePage serves a configurable test page. Pass query parameters to
// flip element states:
//   - ?disabled=1     → button has `disabled` attribute
//   - ?aria=1         → button has aria-disabled="true"
//   - ?pointer=1      → button has CSS pointer-events: none
//   - ?hidden=1       → button has display: none
func actionablePage(state string) string {
	style := ""
	attrs := ""
	switch state {
	case "disabled":
		attrs = " disabled"
	case "aria":
		attrs = ` aria-disabled="true"`
	case "pointer":
		style = " style=\"pointer-events: none\""
	case "hidden":
		style = " style=\"display: none\""
	}
	return fmt.Sprintf(`<!DOCTYPE html><html><body>
<button id="btn"%s%s onclick="document.title='clicked'">DoIt</button>
</body></html>`, attrs, style)
}

// enableOnFirstRejection enables #btn shortly after the gate first rejects it
// and reports whether that happened. (A load-time timer fires before New()
// returns, leaving the gate nothing to wait through.)
func enableOnFirstRejection(t *testing.T, d *Driver) (rejected func() bool) {
	t.Helper()
	_, err := d.page.Eval(`() => {
		const orig = window.__maestro._isActionable;
		window.__maestro._isActionable = function (el) {
			const ok = orig.call(this, el);
			if (!ok && !window.__rejectedOnce) {
				window.__rejectedOnce = true;
				setTimeout(() => el.removeAttribute('disabled'), 100);
			}
			return ok;
		};
	}`)
	if err != nil {
		t.Fatalf("install enable hook: %v", err)
	}
	return func() bool {
		res, err := d.page.Eval(`() => window.__rejectedOnce === true`)
		if err != nil {
			t.Fatalf("read enable hook: %v", err)
		}
		return res.Value.Bool()
	}
}

func newActionableTestServer(html string) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	})
	return httptest.NewServer(mux)
}

// TestIsActionable_HappyPath: a plain visible enabled button reports
// actionable on first probe.
func TestIsActionable_HappyPath(t *testing.T) {
	ts := newActionableTestServer(actionablePage(""))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()

	elem, _, err := d.findElement(flow.Selector{ID: "btn"}, false, 2000)
	if err != nil {
		t.Fatalf("findElement: %v", err)
	}

	res, err := elem.Eval(`() => window.__maestro._isActionable(this)`)
	if err != nil {
		t.Fatalf("eval _isActionable: %v", err)
	}
	if !res.Value.Bool() {
		t.Errorf("expected actionable=true for a plain visible button, got false")
	}
}

// TestIsActionable_Disabled: button with the HTML `disabled` attribute is
// not actionable. The check covers form-control disabled state.
func TestIsActionable_Disabled(t *testing.T) {
	ts := newActionableTestServer(actionablePage("disabled"))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()

	elem, _, err := d.findElement(flow.Selector{ID: "btn"}, false, 2000)
	if err != nil {
		t.Fatalf("findElement: %v", err)
	}
	res, _ := elem.Eval(`() => window.__maestro._isActionable(this)`)
	if res.Value.Bool() {
		t.Errorf("expected actionable=false for disabled button")
	}
}

// TestIsActionable_AriaDisabled: aria-disabled="true" blocks actionability
// even when the HTML disabled attribute is not set (covers ARIA-only
// custom controls).
func TestIsActionable_AriaDisabled(t *testing.T) {
	ts := newActionableTestServer(actionablePage("aria"))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()

	elem, _, err := d.findElement(flow.Selector{ID: "btn"}, false, 2000)
	if err != nil {
		t.Fatalf("findElement: %v", err)
	}
	res, _ := elem.Eval(`() => window.__maestro._isActionable(this)`)
	if res.Value.Bool() {
		t.Errorf("expected actionable=false for aria-disabled button")
	}
}

// TestIsActionable_PointerEventsNone: pointer-events: none blocks
// actionability — covers CSS-disabled controls (common pattern for
// "looks enabled but pre-emptively blocked").
func TestIsActionable_PointerEventsNone(t *testing.T) {
	ts := newActionableTestServer(actionablePage("pointer"))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()

	elem, _, err := d.findElement(flow.Selector{ID: "btn"}, false, 2000)
	if err != nil {
		t.Fatalf("findElement: %v", err)
	}
	res, _ := elem.Eval(`() => window.__maestro._isActionable(this)`)
	if res.Value.Bool() {
		t.Errorf("expected actionable=false when pointer-events: none")
	}
}

// TestWaitForActionable_WaitsForEnable: the gate outwaits a disabled state.
func TestWaitForActionable_WaitsForEnable(t *testing.T) {
	ts := newActionableTestServer(actionablePage("disabled"))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()

	elem, _, err := d.findElement(flow.Selector{ID: "btn"}, false, 2000)
	if err != nil {
		t.Fatalf("findElement: %v", err)
	}
	rejected := enableOnFirstRejection(t, d)

	if err := d.waitForActionable(elem, 2000); err != nil {
		t.Fatalf("expected to become actionable after enable; got: %v", err)
	}
	if !rejected() {
		t.Fatal("gate never saw the disabled state, so it did not wait through it")
	}
}

// TestWaitForActionable_DeadlineBoundsUnansweredProbe: a hung page fails
// the gate at its deadline.
func TestWaitForActionable_DeadlineBoundsUnansweredProbe(t *testing.T) {
	ts := newActionableTestServer(actionablePage("disabled"))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()

	elem, _, err := d.findElement(flow.Selector{ID: "btn"}, false, 2000)
	if err != nil {
		t.Fatalf("findElement: %v", err)
	}
	if _, err := d.page.Eval(`() => { setTimeout(() => { for (;;) {} }, 0); }`); err != nil {
		t.Fatalf("schedule main-thread hang: %v", err)
	}

	const timeoutMs = 300
	start := time.Now()
	err = d.waitForActionable(elem, timeoutMs)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected an error from a page that never answers, got nil")
	}
	if !strings.Contains(err.Error(), "not actionable") {
		t.Errorf("expected error to mention 'not actionable', got: %v", err)
	}
	if limit := timeoutMs*time.Millisecond + time.Second; elapsed > limit {
		t.Errorf("gate returned after %v, past its %dms deadline (limit %v)", elapsed, timeoutMs, limit)
	}
}

// TestWaitForActionable_TimesOut: persistently disabled button never
// becomes actionable; waitForActionable returns a clear error.
func TestWaitForActionable_TimesOut(t *testing.T) {
	ts := newActionableTestServer(actionablePage("disabled"))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()

	elem, _, err := d.findElement(flow.Selector{ID: "btn"}, false, 2000)
	if err != nil {
		t.Fatalf("findElement: %v", err)
	}

	// Tight timeout — element is permanently disabled.
	err = d.waitForActionable(elem, 200)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "not actionable") {
		t.Errorf("expected error to mention 'not actionable', got: %v", err)
	}
}

// TestTapOn_RejectsDisabled: end-to-end through the tapOn command. A
// disabled button must surface an error rather than silently dispatching
// or hanging on the existing find timeout.
func TestTapOn_RejectsDisabled(t *testing.T) {
	ts := newActionableTestServer(actionablePage("disabled"))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()

	// Short timeout so we don't burn the default find budget.
	step := &flow.TapOnStep{Selector: flow.Selector{ID: "btn"}}
	step.TimeoutMs = 500
	res := d.Execute(step)
	if res.Success {
		t.Fatal("expected tapOn on disabled button to fail")
	}
	if !strings.Contains(strings.ToLower(res.Message), "not actionable") {
		t.Errorf("expected 'not actionable' error; got: %s", res.Message)
	}
}

// TestTapOn_PassesAfterEnable: tapOn waits through a disabled state, then taps.
func TestTapOn_PassesAfterEnable(t *testing.T) {
	ts := newActionableTestServer(actionablePage("disabled"))
	defer ts.Close()
	d := newTestDriver(t, ts.URL)
	defer d.Close()
	rejected := enableOnFirstRejection(t, d)

	res := d.Execute(&flow.TapOnStep{Selector: flow.Selector{ID: "btn"}})
	if !res.Success {
		t.Fatalf("expected tapOn to succeed once button enables; got: %s", res.Message)
	}
	if !rejected() {
		t.Fatal("gate never saw the disabled state, so it did not wait through it")
	}
}
