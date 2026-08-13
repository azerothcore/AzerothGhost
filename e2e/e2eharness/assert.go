package e2eharness

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// AC-issue style assertion helpers.
//
// Tests use three severities so CI and authors can tell them apart:
//   - precondition: setup failed — the issue was not evaluated
//   - CONFIRMED BUG: core behaviour is wrong (expected on unfixed AC)
//   - harness failure: infra/timeout/missing unit (not an AC bug report)

// e2eFailPrefix is greppable in CI logs alongside go's "--- FAIL".
const e2eFailPrefix = "E2E_FAIL "

// Preconditionf fails the test: setup did not reach a state where the issue
// can be evaluated. Prefer this over a bare Fatalf for missing NPCs, failed
// auras, etc.
func Preconditionf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(e2eFailPrefix+"precondition: "+format, args...)
}

// ConfirmedBugf fails with the standard AC-issue bug marker.
// issue is the AzerothCore issue or PR number (e.g. 27095).
func ConfirmedBugf(t *testing.T, issue int, format string, args ...any) {
	t.Helper()
	t.Fatalf(e2eFailPrefix+"AC#%d CONFIRMED BUG: "+format, append([]any{issue}, args...)...)
}

// HarnessFailf fails for infra problems (timeouts, cache empty, cast never
// started) that are not themselves the AC bug under test.
func HarnessFailf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(e2eFailPrefix+"harness: "+format, args...)
}

// SoftWarnf logs a non-fatal WARNING (soft-pass diagnostics).
func SoftWarnf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Logf("WARNING: "+format, args...)
}

// SoftPass is disabled by default: it fails the test so suites cannot greenwash
// unjudgeable fixtures. Opt in only for temporary local debugging via
// E2E_ALLOW_SOFT_PASS=1 (still logs SOFT-PASS for grepping).
//
// Prefer Preconditionf / Assertf / ConfirmedBugf instead of SoftPass in new tests.
func SoftPass(t *testing.T, reason string, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	t.Logf("SOFT-PASS reason=%s %s", reason, msg)
	if os.Getenv("E2E_ALLOW_SOFT_PASS") == "1" {
		return
	}
	t.Fatalf(e2eFailPrefix+"SOFT-PASS disabled (set E2E_ALLOW_SOFT_PASS=1 to allow) reason=%s %s", reason, msg)
}

// SoftPassf is SoftPass with reason taken from the format string prefix.
func SoftPassf(t *testing.T, format string, args ...any) {
	t.Helper()
	SoftPass(t, "soft", format, args...)
}

// Assertf fails a post-drive behavioural oracle (not setup). Prefer over
// Preconditionf after the scenario has already been exercised.
func Assertf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(e2eFailPrefix+"assert: "+format, args...)
}

// AssertBugf fails a product oracle. issue must be >0; otherwise Assertf.
func AssertBugf(t *testing.T, issue int, format string, args ...any) {
	t.Helper()
	if issue <= 0 {
		Assertf(t, format, args...)
		return
	}
	ConfirmedBugf(t, issue, format, args...)
}

// Require fatals as a precondition when ok is false.
func Require(t *testing.T, ok bool, format string, args ...any) {
	t.Helper()
	if !ok {
		Preconditionf(t, format, args...)
	}
}

// RequireUnit fatals as a precondition when guid is 0.
func RequireUnit(t *testing.T, guid uint64, what string) {
	t.Helper()
	if guid == 0 {
		Preconditionf(t, "%s not found (guid=0)", what)
	}
}

// IntervalBugOpts configures AssertIntervalNotAccelerated.
type IntervalBugOpts struct {
	// MaxFromEvent is the post-kill (or post-event) window that indicates the bug
	// (default 20s — accelerated ~5s misfire).
	MaxFromEvent time.Duration
	// MaxFromBaseline is the max acceptable gap from the baseline spawn
	// (default 45s — half of a normal ~60s wave).
	MaxFromBaseline time.Duration
}

// AssertIntervalNotAccelerated fails with CONFIRMED BUG when both clocks look early.
// fromEvent is time since the kill/event; fromBaseline is time since the newer wave spawn.
func AssertIntervalNotAccelerated(t *testing.T, issue int, fromEvent, fromBaseline time.Duration, opts IntervalBugOpts) {
	t.Helper()
	maxEv := opts.MaxFromEvent
	if maxEv <= 0 {
		maxEv = 20 * time.Second
	}
	maxBase := opts.MaxFromBaseline
	if maxBase <= 0 {
		maxBase = 45 * time.Second
	}
	if fromEvent < maxEv && fromBaseline < maxBase {
		ConfirmedBugf(t, issue,
			"next event after %s (<%s) and only %s after baseline (<%s) — timer accelerated",
			fromEvent.Round(time.Millisecond), maxEv,
			fromBaseline.Round(time.Millisecond), maxBase)
	}
	if fromBaseline < maxBase {
		SoftWarnf(t, "interval only %s after baseline (expected ~60s) but outside quick post-event window",
			fromBaseline.Round(time.Millisecond))
	}
	t.Logf("interval OK (fromEvent=%s fromBaseline=%s)",
		fromEvent.Round(time.Millisecond), fromBaseline.Round(time.Millisecond))
}

// FormatGUID is a short hex GUID for logs.
func FormatGUID(guid uint64) string {
	return fmt.Sprintf("0x%X", guid)
}
