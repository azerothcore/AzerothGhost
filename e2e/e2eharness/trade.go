package e2eharness

import (
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// DefaultTradeTimeout is used by trade waiters when timeout <= 0.
const DefaultTradeTimeout = 10 * time.Second

// WaitTradeStatus waits until OnTradeStatus delivers a matching status (or any if want==^0).
// Arm before the action that produces the status when possible (FormParty-style).
func (b *ScenarioBot) WaitTradeStatus(t *testing.T, want uint32, timeout time.Duration) client.TradeStatusInfo {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultTradeTimeout
	}
	ch := make(chan client.TradeStatusInfo, 8)
	cancel := b.World.AddTradeStatusHook(func(info client.TradeStatusInfo) {
		if want == ^uint32(0) || info.Status == want {
			select {
			case ch <- info:
			default:
			}
		}
	})
	defer cancel()

	// Already have it?
	last := b.World.LastTradeStatus()
	if want != ^uint32(0) && last.Status == want {
		return last
	}
	if want == client.TradeStatusOpenWindow && b.World.TradeOpen() {
		return last
	}

	deadline := time.After(timeout)
	for {
		select {
		case info := <-ch:
			return info
		case <-deadline:
			last = b.World.LastTradeStatus()
			if want == client.TradeStatusOpenWindow && b.World.TradeOpen() {
				return last
			}
			HarnessFailf(t, "WaitTradeStatus want=%s (%d) timeout; last=%s (%d) open=%v",
				client.TradeStatusName(want), want, client.TradeStatusName(last.Status), last.Status, b.World.TradeOpen())
			return last
		}
	}
}

// WaitTradeOpen waits until TradeOpen() is true or last status is OPEN_WINDOW.
func (b *ScenarioBot) WaitTradeOpen(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultTradeTimeout
	}
	if b.World.TradeOpen() || b.World.LastTradeStatus().Status == client.TradeStatusOpenWindow {
		return
	}
	b.WaitTradeStatus(t, client.TradeStatusOpenWindow, timeout)
	if !b.World.TradeOpen() && b.World.LastTradeStatus().Status != client.TradeStatusOpenWindow {
		HarnessFailf(t, "WaitTradeOpen: TradeOpen=false last=%s",
			client.TradeStatusName(b.World.LastTradeStatus().Status))
	}
}

// InitiateTrade opens a trade with other (sends CMSG_INITIATE_TRADE).
// The other bot must call AcceptTradeWindow (BEGIN_TRADE → CMSG_BEGIN_TRADE).
func (b *ScenarioBot) InitiateTrade(t *testing.T, other *ScenarioBot) {
	t.Helper()
	if other == nil || other.GUID == 0 {
		Preconditionf(t, "InitiateTrade: other GUID is 0")
	}
	if err := b.World.InitiateTrade(other.GUID); err != nil {
		HarnessFailf(t, "InitiateTrade: %v", err)
	}
}

// AcceptTradeWindow handles TRADE_STATUS_BEGIN_TRADE by sending CMSG_BEGIN_TRADE
// and waits for TRADE_STATUS_OPEN_WINDOW on this bot.
// Prefer OpenTrade which arms before initiate (no race / no sleep).
func (b *ScenarioBot) AcceptTradeWindow(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultTradeTimeout
	}
	// Wait for begin invitation, then begin.
	info := b.WaitTradeStatus(t, client.TradeStatusBeginTrade, timeout)
	if info.Status != client.TradeStatusBeginTrade {
		Preconditionf(t, "expected BEGIN_TRADE, got %s", client.TradeStatusName(info.Status))
	}
	if err := b.World.BeginTrade(); err != nil {
		HarnessFailf(t, "BeginTrade: %v", err)
	}
	b.WaitTradeOpen(t, timeout)
}

// reseatTradePair places both bots within TRADE_DISTANCE on the package pad and combatstops.
// Skips a far tele when already co-located (avoids transfer races right before initiate).
func reseatTradePair(t *testing.T, a, b *ScenarioBot) {
	t.Helper()
	if a == nil || b == nil {
		return
	}
	ax, ay, az, am := a.Pos()
	bx, by, bz, bm := b.Pos()
	if am == bm && Distance3D(ax, ay, az, bx, by, bz) <= 8 {
		a.CombatStop(t)
		b.CombatStop(t)
		return
	}
	pad := PackagePad(t)
	a.Teleport(t, pad.X, pad.Y, pad.Z, pad.Map)
	b.Teleport(t, pad.X+1.5, pad.Y, pad.Z, pad.Map)
	a.CombatStop(t)
	b.CombatStop(t)
}

// clearOpenTrades cancels only when the client believes a window is open.
//
// Never send a speculative CMSG_CANCEL_TRADE on a quiet bot: world sessions process
// packets independently, so a late Cancel on the target can run *after* the
// initiator's InitiateTrade and destroy the new TradeData (BEGIN_TRADE then
// TRADE_CANCELED with no OPEN_WINDOW).
func clearOpenTrades(t *testing.T, bots ...*ScenarioBot) {
	t.Helper()
	any := false
	for _, b := range bots {
		if b == nil || b.World == nil {
			continue
		}
		if b.World.TradeOpen() {
			_ = b.World.CancelTrade()
			any = true
		}
	}
	if any {
		// Let both sessions finish TradeCancel before the next InitiateTrade.
		time.Sleep(300 * time.Millisecond)
	}
}

// OpenTrade: initiator InitiateTrade → target BEGIN_TRADE → CMSG_BEGIN_TRADE → both OPEN_WINDOW.
// Retries under suite load. Failures report both bots' last statuses; TradeStatusBusy==0 so
// "none" is used when no SMSG_TRADE_STATUS was ever seen.
func OpenTrade(t *testing.T, initiator, target *ScenarioBot) {
	t.Helper()
	if initiator == nil || target == nil {
		HarnessFailf(t, "OpenTrade: nil bot")
	}

	// Stay GM-on for pad invuln (no IsGameMaster trade block on AC).
	initiator.GM(t, ".gm on")
	target.GM(t, ".gm on")
	clearOpenTrades(t, initiator, target)
	reseatTradePair(t, initiator, target)

	const maxAttempts = 5
	var lastTgt, lastIni client.TradeStatusInfo
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			t.Logf("OpenTrade retry %d: tgtLast=%s iniLast=%s openT=%v openI=%v",
				attempt,
				client.TradeStatusName(lastTgt.Status), client.TradeStatusName(lastIni.Status),
				target.World.TradeOpen(), initiator.World.TradeOpen())
			clearOpenTrades(t, initiator, target)
			pad := PackagePad(t)
			initiator.Teleport(t, pad.X, pad.Y, pad.Z, pad.Map)
			target.Teleport(t, pad.X+1.5, pad.Y, pad.Z, pad.Map)
			initiator.CombatStop(t)
			target.CombatStop(t)
			time.Sleep(250 * time.Millisecond)
		}

		beginCh := make(chan client.TradeStatusInfo, 2)
		cancelBegin := target.World.AddTradeStatusHook(func(info client.TradeStatusInfo) {
			if info.Status == client.TradeStatusBeginTrade {
				select {
				case beginCh <- info:
				default:
				}
			}
		})
		failCh := make(chan client.TradeStatusInfo, 4)
		cancelFail := initiator.World.AddTradeStatusHook(func(info client.TradeStatusInfo) {
			switch info.Status {
			case client.TradeStatusBusy, client.TradeStatusBusy2, client.TradeStatusNoTarget,
				client.TradeStatusTargetTooFar, client.TradeStatusWrongFaction,
				client.TradeStatusYouDead, client.TradeStatusTargetDead,
				client.TradeStatusYouStunned, client.TradeStatusTargetStunned,
				client.TradeStatusTradeCanceled, client.TradeStatusCloseWindow:
				select {
				case failCh <- info:
				default:
				}
			}
		})

		seqBefore := target.World.TradeStatusSeq()
		initiator.InitiateTrade(t, target)

		gotBegin := false
		deadline := time.Now().Add(4 * time.Second)
		for time.Now().Before(deadline) && !gotBegin {
			select {
			case <-beginCh:
				gotBegin = true
			case fail := <-failCh:
				lastIni = fail
				// Record only — BEGIN may still arrive; wait until timeout.
			case <-time.After(40 * time.Millisecond):
				if target.World.TradeStatusSeq() > seqBefore &&
					target.World.LastTradeStatus().Status == client.TradeStatusBeginTrade {
					gotBegin = true
				}
			}
		}
		cancelBegin()
		cancelFail()
		lastTgt = target.World.LastTradeStatus()
		if lastIni.Status == 0 && initiator.World.TradeStatusSeen() {
			lastIni = initiator.World.LastTradeStatus()
		}

		if !gotBegin {
			clearOpenTrades(t, initiator, target)
			continue
		}

		// BEGIN received — open immediately (no other bot traffic first).
		if err := target.World.BeginTrade(); err != nil {
			HarnessFailf(t, "OpenTrade BeginTrade: %v", err)
		}
		openDeadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(openDeadline) {
			if initiator.World.TradeOpen() && target.World.TradeOpen() {
				return
			}
			st := target.World.LastTradeStatus().Status
			if st == client.TradeStatusTradeCanceled || st == client.TradeStatusCloseWindow ||
				st == client.TradeStatusTargetTooFar {
				break
			}
			time.Sleep(40 * time.Millisecond)
		}
		if initiator.World.TradeOpen() && target.World.TradeOpen() {
			return
		}
		lastTgt = target.World.LastTradeStatus()
		lastIni = initiator.World.LastTradeStatus()
		clearOpenTrades(t, initiator, target)
	}

	ix, iy, iz, im := initiator.Pos()
	tx, ty, tz, tm := target.Pos()
	tgtLabel := client.TradeStatusName(lastTgt.Status)
	if !target.World.TradeStatusSeen() {
		tgtLabel = "none"
	}
	iniLabel := client.TradeStatusName(lastIni.Status)
	if !initiator.World.TradeStatusSeen() {
		iniLabel = "none"
	}
	HarnessFailf(t, "OpenTrade: %s failed after %d attempts (tgtLast=%s iniLast=%s d=%.1f mapI=%d mapT=%d)",
		target.Name, maxAttempts, tgtLabel, iniLabel,
		Distance3D(ix, iy, iz, tx, ty, tz), im, tm)
}

// SetTradeItem places an inventory item into trade slot.
// Server unaccepts both sides (BACK_TO_TRADE). Must wait for that status — not OPEN_WINDOW
// from OpenTrade — or CompleteTrade accepts before the item lands and races unaccept.
func (b *ScenarioBot) SetTradeItem(t *testing.T, tradeSlot, bag, invSlot uint8) {
	t.Helper()
	before := b.World.LastTradeStatus().Status
	if err := b.World.SetTradeItem(tradeSlot, bag, invSlot); err != nil {
		HarnessFailf(t, "SetTradeItem: %v", err)
	}
	waitTradeMutated(t, b, before, 3*time.Second)
}

// SetTradeGold sets offered copper in the open trade.
// Same settle wait as SetTradeItem (server unaccepts → BACK_TO_TRADE).
func (b *ScenarioBot) SetTradeGold(t *testing.T, copper uint32) {
	t.Helper()
	before := b.World.LastTradeStatus().Status
	if err := b.World.SetTradeGold(copper); err != nil {
		HarnessFailf(t, "SetTradeGold: %v", err)
	}
	waitTradeMutated(t, b, before, 3*time.Second)
}

// waitTradeMutated waits until LastTradeStatus changes from beforeStatus, preferably
// to BACK_TO_TRADE (SetItem/SetMoney always unaccept). Soft timeout keeps trade open.
func waitTradeMutated(t *testing.T, b *ScenarioBot, beforeStatus uint32, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	// If already BACK_TO_TRADE from a prior mutation, still require a fresh transition
	// or a brief stable open window after the send.
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st := b.World.LastTradeStatus().Status
		if st == client.TradeStatusBackToTrade {
			return
		}
		// Status advanced past the pre-mutation snapshot (e.g. cancel / complete).
		if st != beforeStatus && st != client.TradeStatusOpenWindow && st != client.TradeStatusBeginTrade {
			return
		}
		if !b.World.TradeOpen() {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	// Soft: item may have applied without a status we observe (SMSG_TRADE_STATUS_EXTENDED only).
	t.Logf("waitTradeMutated: soft timeout last=%s before=%s open=%v",
		client.TradeStatusName(b.World.LastTradeStatus().Status),
		client.TradeStatusName(beforeStatus), b.World.TradeOpen())
}

// AcceptTrade clicks accept on the open trade window.
func (b *ScenarioBot) AcceptTrade(t *testing.T) {
	t.Helper()
	if err := b.World.AcceptTrade(); err != nil {
		HarnessFailf(t, "AcceptTrade: %v", err)
	}
}

// CancelTrade cancels the open trade.
func (b *ScenarioBot) CancelTrade(t *testing.T) {
	t.Helper()
	if err := b.World.CancelTrade(); err != nil {
		HarnessFailf(t, "CancelTrade: %v", err)
	}
}

// WaitTradeComplete waits for TRADE_STATUS_TRADE_COMPLETE.
func (b *ScenarioBot) WaitTradeComplete(t *testing.T, timeout time.Duration) {
	t.Helper()
	b.WaitTradeStatus(t, client.TradeStatusTradeComplete, timeout)
}

// tradeTerminalCancel reports whether a status ends the trade without completion.
func tradeTerminalCancel(st uint32) bool {
	switch st {
	case client.TradeStatusTradeCanceled, client.TradeStatusTargetTooFar,
		client.TradeStatusCloseWindow, client.TradeStatusTradeRejected,
		client.TradeStatusNoTarget, client.TradeStatusYouLogout,
		client.TradeStatusTargetLogout, client.TradeStatusYouDead,
		client.TradeStatusTargetDead:
		return true
	default:
		return false
	}
}

// WaitTradeCancelled waits for cancel / far / close-class terminal statuses,
// or until TradeOpen becomes false (server may drop the window without a packet
// we observe across map transfers).
func (b *ScenarioBot) WaitTradeCancelled(t *testing.T, timeout time.Duration) client.TradeStatusInfo {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultTradeTimeout
	}
	ch := make(chan client.TradeStatusInfo, 8)
	cancel := b.World.AddTradeStatusHook(func(info client.TradeStatusInfo) {
		if tradeTerminalCancel(info.Status) {
			select {
			case ch <- info:
			default:
			}
		}
	})
	defer cancel()

	last := b.World.LastTradeStatus()
	if tradeTerminalCancel(last.Status) {
		return last
	}
	if !b.World.TradeOpen() && last.Status != client.TradeStatusOpenWindow &&
		last.Status != client.TradeStatusBeginTrade && last.Status != client.TradeStatusTradeAccept &&
		last.Status != client.TradeStatusBackToTrade {
		return last
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case info := <-ch:
			return info
		default:
		}
		// Map transfer / server-side cleanup often leaves TradeOpen false without a new status.
		if !b.World.TradeOpen() {
			last = b.World.LastTradeStatus()
			// If we never saw OPEN, or we already advanced past it, treat as cancelled.
			if last.Status != client.TradeStatusOpenWindow && last.Status != client.TradeStatusTradeAccept {
				return last
			}
			// Still last OPEN_WINDOW but flag cleared — synthesize cancel view.
			last.Status = client.TradeStatusTradeCanceled
			return last
		}
		time.Sleep(40 * time.Millisecond)
	}
	HarnessFailf(t, "WaitTradeCancelled timeout last=%s open=%v",
		client.TradeStatusName(b.World.LastTradeStatus().Status), b.World.TradeOpen())
	return b.World.LastTradeStatus()
}

// armTradeComplete installs a TRADE_COMPLETE hook and returns a channel + cancel.
func armTradeComplete(b *ScenarioBot) (ch chan client.TradeStatusInfo, undo func()) {
	ch = make(chan client.TradeStatusInfo, 1)
	cancel := b.World.AddTradeStatusHook(func(info client.TradeStatusInfo) {
		if info.Status == client.TradeStatusTradeComplete {
			select {
			case ch <- info:
			default:
			}
		}
	})
	// Already complete?
	if b.World.LastTradeStatus().Status == client.TradeStatusTradeComplete {
		select {
		case ch <- b.World.LastTradeStatus():
		default:
		}
	}
	return ch, cancel
}

// CompleteTrade dual-accepts an open trade and waits for TRADE_COMPLETE on both sides.
//
// Protocol (AC TradeHandler): first AcceptTrade marks that side accepted and notifies the
// partner with TRADE_ACCEPT; second AcceptTrade while the partner is still accepted runs
// the complete path. We arm complete handlers first, then send both AcceptTrade packets
// back-to-back (no fixed sleep). Retries on BACK_TO_TRADE while the window stays open
// (item/gold settle races or TRADE_DISTANCE checks under pad noise).
func CompleteTrade(t *testing.T, a, b *ScenarioBot) {
	t.Helper()
	chA, undoA := armTradeComplete(a)
	chB, undoB := armTradeComplete(b)
	defer undoA()
	defer undoB()

	gotA, gotB := false, false
	noteComplete := func() {
		select {
		case <-chA:
			gotA = true
		default:
		}
		select {
		case <-chB:
			gotB = true
		default:
		}
		if a.World.LastTradeStatus().Status == client.TradeStatusTradeComplete {
			gotA = true
		}
		if b.World.LastTradeStatus().Status == client.TradeStatusTradeComplete {
			gotB = true
		}
	}

	const maxAttempts = 6
	for attempt := 0; attempt < maxAttempts; attempt++ {
		noteComplete()
		if gotA && gotB {
			return
		}
		if !a.World.TradeOpen() && !b.World.TradeOpen() {
			// Give late complete packets a moment.
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				noteComplete()
				if gotA && gotB {
					return
				}
				time.Sleep(40 * time.Millisecond)
			}
			break
		}

		if attempt > 0 {
			a.CombatStop(t)
			b.CombatStop(t)
		}

		// Dual accept: both CMSG_ACCEPT_TRADE; order is immaterial if both land while open.
		a.AcceptTrade(t)
		b.AcceptTrade(t)

		// Wait for complete or for a terminal non-complete state.
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			noteComplete()
			if gotA && gotB {
				return
			}
			// Partner-accept notification only — keep waiting for the other Accept to land.
			time.Sleep(40 * time.Millisecond)
		}
		t.Logf("CompleteTrade attempt %d: lastA=%s lastB=%s openA=%v openB=%v",
			attempt+1,
			client.TradeStatusName(a.World.LastTradeStatus().Status),
			client.TradeStatusName(b.World.LastTradeStatus().Status),
			a.World.TradeOpen(), b.World.TradeOpen())
	}

	noteComplete()
	if gotA && gotB {
		return
	}
	HarnessFailf(t, "CompleteTrade timeout: a_complete=%v b_complete=%v lastA=%s lastB=%s openA=%v openB=%v",
		gotA, gotB,
		client.TradeStatusName(a.World.LastTradeStatus().Status),
		client.TradeStatusName(b.World.LastTradeStatus().Status),
		a.World.TradeOpen(), b.World.TradeOpen())
}
