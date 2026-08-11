package e2eharness

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// MaxParallelLogins caps concurrent bot logins to avoid authserver/world thrash.
const MaxParallelLogins = 5

// BotIdent is a unique account + character name pair for one e2e bot.
// Race/Class are optional (0 = Human Warrior defaults at login).
type BotIdent struct {
	Account  string
	CharName string
	Race     uint8
	Class    uint8
}

// MakeBotIdents builds n unique account/char pairs with a shared run suffix.
// Character names are pure letters (server rejects digits as mixed languages).
func MakeBotIdents(prefix string, n int) []BotIdent {
	// Use a wide unique token so accounts never collide across rapid test re-runs.
	// Accounts may contain digits; character names may not.
	runID := time.Now().UnixNano()
	out := make([]BotIdent, n)
	for i := 0; i < n; i++ {
		out[i].Account = fmt.Sprintf("%s%02d%x", prefix, i, runID&0xFFFFFFFF)
		charPrefix := "Lead"
		if i > 0 {
			charPrefix = fmt.Sprintf("Sig%c", 'a'+byte((i-1)%26))
		}
		if len(prefix) >= 2 {
			charPrefix = SanitizeCharName(prefix[:min(3, len(prefix))] + charPrefix)
		}
		out[i].CharName = SanitizeCharName(charPrefix + Base26(uint64(runID)+uint64(i)*97, 5))
	}
	return out
}

// EnsureBotAccounts creates accounts and sets GM level 3 for each identity.
func EnsureBotAccounts(t *testing.T, authDB *sql.DB, idents []BotIdent) {
	t.Helper()
	for _, id := range idents {
		if err := EnsureAccount(authDB, id.Account, DefaultPassword); err != nil {
			t.Fatalf("ensure account %s: %v", id.Account, err)
		}
		if err := SetGM(authDB, id.Account, 3); err != nil {
			t.Fatalf("set gm %s: %v", id.Account, err)
		}
	}
}

// LoginAllianceBots logs in bots via LoginBots, defaulting each identity to
// Human Warrior when Race/Class are zero. Prefer LoginBots for race-aware scenarios.
func LoginAllianceBots(t *testing.T, idents []BotIdent) []*Session {
	t.Helper()
	normalized := make([]BotIdent, len(idents))
	for i, id := range idents {
		normalized[i] = id
		if normalized[i].Race == 0 {
			normalized[i].Race = RaceHuman
		}
		if normalized[i].Class == 0 {
			normalized[i].Class = ClassWarrior
		}
	}
	return LoginBots(t, normalized)
}

// LoginAllianceBotsNoCleanup is like LoginAllianceBots but does not register
// t.Cleanup(Close). Callers (package-shared fixtures) must Close sessions themselves.
func LoginAllianceBotsNoCleanup(t *testing.T, idents []BotIdent) []*Session {
	t.Helper()
	normalized := make([]BotIdent, len(idents))
	for i, id := range idents {
		normalized[i] = id
		if normalized[i].Race == 0 {
			normalized[i].Race = RaceHuman
		}
		if normalized[i].Class == 0 {
			normalized[i].Class = ClassWarrior
		}
	}
	return loginBots(t, normalized, false)
}

// CleanupSessionsGuildState clears guild/petition leftovers for every session.
func CleanupSessionsGuildState(t *testing.T, charDB *sql.DB, sessions []*Session) {
	t.Helper()
	for _, s := range sessions {
		CleanupGuildForLeader(charDB, s.GUID)
		if err := CleanupPlayer(charDB, s.GUID); err != nil {
			t.Fatalf("cleanup %s guid=%d: %v", s.Name, s.GUID, err)
		}
	}
}

// UniqueGuildName returns a short unique guild name for this run.
func UniqueGuildName(prefix string) string {
	if prefix == "" {
		prefix = "G"
	}
	// Keep short — guild names have length limits.
	return fmt.Sprintf("%s%d", prefix, time.Now().Unix()%1000000)
}

// OpenTestDBs opens auth + character DBs or fails the test.
func OpenTestDBs(t *testing.T) (authDB, charDB *sql.DB) {
	t.Helper()
	var err error
	authDB, err = OpenAuthDB()
	if err != nil {
		t.Fatalf("auth db: %v", err)
	}
	t.Cleanup(func() { _ = authDB.Close() })
	charDB, err = OpenCharDB()
	if err != nil {
		t.Fatalf("char db: %v", err)
	}
	t.Cleanup(func() { _ = charDB.Close() })
	return authDB, charDB
}

// SetupGuildLeader creates a full guild (1 leader + MinPetitionSigns signers) via
// the charter protocol and returns the leader session + setup metadata.
// Use for bank-focused tests that need a guild but are not testing charter itself.
// Sessions are closed via t.Cleanup. For package-shared bank fixtures prefer
// SetupGuildLeaderKeepAlive + explicit Close of returned sessions.
func SetupGuildLeader(t *testing.T, accountPrefix string) (leader *Session, setup GuildSetup, charDB *sql.DB) {
	t.Helper()
	authDB, charDB := OpenTestDBs(t)
	n := 1 + MinPetitionSigns
	idents := MakeBotIdents(accountPrefix, n)
	EnsureBotAccounts(t, authDB, idents)
	sessions := LoginAllianceBots(t, idents)
	CleanupSessionsGuildState(t, charDB, sessions)
	guildName := UniqueGuildName(accountPrefix)
	setup = CreateGuildViaCharter(t, sessions[0], sessions[1:], charDB, guildName, MinPetitionSigns)
	// Bank tests only need the leader online; free signer world connections early.
	for _, s := range sessions[1:] {
		s.Close()
	}
	return sessions[0], setup, charDB
}

// SetupGuildLeaderKeepAlive is like SetupGuildLeader but does not register
// t.Cleanup on sessions or DBs, and closes signers after turn-in (leader stays up).
// Caller must Close the returned leader and DBs (e.g. from TestMain).
func SetupGuildLeaderKeepAlive(t *testing.T, accountPrefix string) (leader *Session, setup GuildSetup, authDB, charDB *sql.DB) {
	t.Helper()
	var err error
	authDB, err = OpenAuthDB()
	if err != nil {
		t.Fatalf("auth db: %v", err)
	}
	charDB, err = OpenCharDB()
	if err != nil {
		_ = authDB.Close()
		t.Fatalf("char db: %v", err)
	}
	n := 1 + MinPetitionSigns
	idents := MakeBotIdents(accountPrefix, n)
	EnsureBotAccounts(t, authDB, idents)
	sessions := LoginAllianceBotsNoCleanup(t, idents)
	CleanupSessionsGuildState(t, charDB, sessions)
	guildName := UniqueGuildName(accountPrefix)
	setup = CreateGuildViaCharter(t, sessions[0], sessions[1:], charDB, guildName, MinPetitionSigns)
	for _, s := range sessions[1:] {
		s.Close()
	}
	return sessions[0], setup, authDB, charDB
}
