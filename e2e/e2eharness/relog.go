package e2eharness

import (
	"testing"
	"time"
)

// Relog logs the bot out of the world and back in on the same account/character.
// Rebinds b.Session fields; keeps AuthDB, CharDB, Ident, Role.
// Registers the new session with t.Cleanup via LoginBots.
//
// The old session is Closed; do not use the previous *Session pointer afterward
// (the ScenarioBot pointer itself is updated in place).
func (b *ScenarioBot) Relog(t *testing.T) {
	t.Helper()
	account := b.Ident.Account
	charName := b.Ident.CharName
	if charName == "" && b.Session != nil {
		charName = b.Name
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
	b.Ident.CharName = charName
	b.Ident.Race = race
	b.Ident.Class = class
	if s.GUID != oldGUID && s.GUID != 0 && oldGUID != 0 {
		t.Logf("WARNING: relog GUID 0x%X != original 0x%X", s.GUID, oldGUID)
	}
	t.Logf("relogged %s char=%s guid=0x%X", account, charName, s.GUID)
}
