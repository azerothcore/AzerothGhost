package e2eharness

import (
	"testing"
	"time"

	"github.com/azerothcore/AzerothGhost/client"
)

// DefaultInWorldTimeout is used by WaitInWorld when timeout <= 0.
const DefaultInWorldTimeout = 30 * time.Second

// WaitInWorld blocks until the bot is in PhaseInWorld (gameplay-ready).
// Use after Relog, far .tele / map transfer, or any path that may leave the
// session in PhaseFarTransfer / PhaseNearTeleport / PhaseLoading.
// Prefer this over fixed sleeps after login or teleport.
func (b *ScenarioBot) WaitInWorld(t *testing.T, timeout time.Duration) {
	t.Helper()
	if b == nil || b.World == nil {
		HarnessFailf(t, "WaitInWorld: nil bot/world")
	}
	if timeout <= 0 {
		timeout = DefaultInWorldTimeout
	}
	if b.World.IsInWorld() {
		return
	}
	if err := b.World.WaitForSessionPhase(client.PhaseInWorld, timeout); err != nil {
		HarnessFailf(t, "%s: WaitInWorld: %v (phase=%s)", b.Name, err, b.World.SessionPhase())
	}
}

// WaitInWorld is the package-level helper for a bare WorldClient.
func WaitInWorld(t *testing.T, w *client.WorldClient, timeout time.Duration) {
	t.Helper()
	if w == nil {
		HarnessFailf(t, "WaitInWorld: nil world")
	}
	if timeout <= 0 {
		timeout = DefaultInWorldTimeout
	}
	if w.IsInWorld() {
		return
	}
	if err := w.WaitForSessionPhase(client.PhaseInWorld, timeout); err != nil {
		HarnessFailf(t, "WaitInWorld: %v (phase=%s)", err, w.SessionPhase())
	}
}

// Relog logs the bot out of the world and back in on the same account/character.
// Rebinds b.Session fields; keeps AuthDB, CharDB, Ident, Role.
// Registers the new session with t.Cleanup via LoginBots.
// Waits until the new session is PhaseInWorld before returning.
//
// The old session is Closed; do not use the previous *Session pointer afterward
// (the ScenarioBot pointer itself is updated in place).
func (b *ScenarioBot) Relog(t *testing.T) {
	t.Helper()
	account := b.Ident.Account
	// Use the character actually in-world. Ident.CharName is the *requested*
	// create name and can differ when UniqueLetterNames had to skip CHAR_NAME_THREE_CONSECUTIVE.
	charName := b.Name
	if charName == "" {
		charName = b.Ident.CharName
	}
	race, class := b.Ident.Race, b.Ident.Class
	if race == 0 {
		race = RaceHuman
	}
	if class == 0 {
		class = ClassWarrior
	}
	oldGUID := b.GUID

	if b.World != nil {
		_ = b.World.SendLogout()
		if err := b.World.WaitForLogout(30 * time.Second); err != nil {
			t.Logf("logout wait: %v (continuing with Close)", err)
		}
	}
	b.Close()

	idents := []BotIdent{{
		Account:  account,
		CharName: charName,
		Race:     race,
		Class:    class,
	}}
	sessions := LoginBots(t, idents)
	s := sessions[0]
	b.Session = s
	b.Ident.Account = account
	b.Ident.CharName = s.Name
	b.Ident.Race = race
	b.Ident.Class = class
	if oldGUID != 0 && s.GUID != oldGUID {
		HarnessFailf(t, "relog entered a different character (guid=0x%X want 0x%X name=%q requested=%q)",
			s.GUID, oldGUID, s.Name, charName)
	}
	// LoginBots already waits for login; re-assert PhaseInWorld for authors.
	b.WaitInWorld(t, DefaultInWorldTimeout)
	t.Logf("relogged %s char=%s guid=0x%X phase=%s", account, s.Name, s.GUID, b.World.SessionPhase())
}

// HardDisconnect forcibly closes the world socket without CMSG_LOGOUT_REQUEST /
// WaitForLogout. Use for crash / "session drop" probes (charm logout, vehicle
// passenger disconnect, SESS hard-drop). Prefer Relog for graceful logout+reenter.
//
// Semantics:
//   - No logout packet is sent — server sees an abrupt TCP drop.
//   - The bot's Session is unusable afterward (do not Cast/GM on it).
//   - Probe world health with a *different* bot via ProbeWorldAlive / AssertWorldAlive.
//
// CloseHard is an alias with the same no-logout semantics.
func (b *ScenarioBot) HardDisconnect(t *testing.T) {
	t.Helper()
	if b == nil || b.Session == nil {
		return
	}
	name := b.Name
	if name == "" {
		name = b.Ident.CharName
	}
	t.Logf("%s HardDisconnect (socket close, no logout packet)", name)
	b.Session.Close()
}

// CloseHard is HardDisconnect — explicit name for authors comparing graceful Close paths.
// Does NOT send logout; see HardDisconnect.
func (b *ScenarioBot) CloseHard(t *testing.T) {
	t.Helper()
	b.HardDisconnect(t)
}
