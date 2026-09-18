package e2eharness

import (
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/azerothcore/AzerothGhost/client"
)

// ArenaBattlemasterEntry is the "Arena Battlemaster" creature template.
const ArenaBattlemasterEntry uint32 = 26007

// ArenaTournamentEvent gates every Arena Battlemaster spawn and is off by default, so
// the NPC has to be brought into the world before anyone can queue at one.
const ArenaTournamentEvent = 31

// DefaultBattlefieldTimeout is used by battlefield waiters when timeout <= 0. The rated
// queue is swept every 5s, hardcoded as BattlegroundMgr::Update's
// m_NextPeriodicQueueUpdateTime rather than configurable, so this is several sweeps.
const DefaultBattlefieldTimeout = 20 * time.Second

// MinSeedableRating and MaxSeedableRating bound the ratings SeedMatchmakerRating accepts.
// Zero is out because it reads back the same as a character with no rating at all.
const (
	MinSeedableRating = 1
	MaxSeedableRating = 32767
)

// arenaQueueLeaveTimeout bounds the wait for the realm's answer to a leave. Short because
// it runs in cleanup, where the time is charged to the next test.
const arenaQueueLeaveTimeout = 5 * time.Second

// arenaStatsSettleDelay separates the two cleanup passes over character_arena_stats.
const arenaStatsSettleDelay = 300 * time.Millisecond

// WaitBattlefieldStatus waits until the bot has received an SMSG_BATTLEFIELD_STATUS
// carrying the given status (client.BattlegroundStatusWaitQueue / WaitJoin / InProgress)
// and returns it.
//
// It scans every status received since login or the last DrainBattlefieldStatuses, so
// a status that arrived before the call still matches. A queue is a sequence (WAIT_QUEUE,
// then WAIT_JOIN on the next matchmaker sweep, which can be immediate), and waiting for
// each in turn must not lose the second one. Drain before re-queueing.
func (b *ScenarioBot) WaitBattlefieldStatus(t *testing.T, status uint32, timeout time.Duration) client.BattlefieldStatus {
	t.Helper()
	st, ok := b.TryWaitBattlefieldStatus(status, timeout)
	if !ok {
		HarnessFailf(t, "%s: no SMSG_BATTLEFIELD_STATUS %s within %s (%s)",
			b.Name, client.BattlegroundStatusName(status), timeout, b.battlefieldDiagnostics())
	}
	return st
}

// TryWaitBattlefieldStatus is WaitBattlefieldStatus without the failure, for callers
// that treat a missing status as an outcome rather than an error.
func (b *ScenarioBot) TryWaitBattlefieldStatus(status uint32, timeout time.Duration) (client.BattlefieldStatus, bool) {
	if timeout <= 0 {
		timeout = DefaultBattlefieldTimeout
	}
	deadline := time.Now().Add(timeout)
	for {
		for _, st := range b.World.BattlefieldStatuses() {
			if st.Status == status {
				return st, true
			}
		}
		if time.Now().After(deadline) {
			return client.BattlefieldStatus{}, false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TryWaitBattlefieldStatusEach is TryWaitBattlefieldStatus for several bots at once, with
// the results indexed like bots. Bots in one bracket watch the same queue sweeps, so the
// ones that never get the status cost one timeout in total rather than one each.
func TryWaitBattlefieldStatusEach(bots []*ScenarioBot, status uint32, timeout time.Duration) ([]client.BattlefieldStatus, []bool) {
	statuses := make([]client.BattlefieldStatus, len(bots))
	oks := make([]bool, len(bots))

	var wg sync.WaitGroup
	for i, b := range bots {
		wg.Go(func() {
			statuses[i], oks[i] = b.TryWaitBattlefieldStatus(status, timeout)
		})
	}
	wg.Wait()
	return statuses, oks
}

// DrainBattlefieldStatuses forgets the statuses received so far, so the next
// WaitBattlefieldStatus only matches packets from the next queue.
func (b *ScenarioBot) DrainBattlefieldStatuses() {
	b.World.DrainBattlefieldStatuses()
}

// battlefieldDiagnostics summarises what the server did say, for failure messages.
func (b *ScenarioBot) battlefieldDiagnostics() string {
	var parts []string
	if last, ok := b.World.LastBattlefieldStatus(); ok {
		parts = append(parts, "last status "+client.BattlegroundStatusName(last.Status))
	} else {
		parts = append(parts, "no status received")
	}
	if r, ok := b.World.LastBattlegroundJoinResult(); ok && r.Refused() {
		parts = append(parts, "join refused: "+client.BattlegroundJoinResultName(r.Result))
	}
	if teamType := b.World.LastArenaError(); teamType != 0 {
		parts = append(parts, fmt.Sprintf("not in a %dv%d arena team", teamType, teamType))
	}
	return strings.Join(parts, ", ")
}

// JoinRatedArena sends CMSG_BATTLEMASTER_JOIN_ARENA as a party leader.
//
// The battlemaster must be in the leader's own map, so place the whole party at one
// first (TeleportToArenaBattlemaster). A rated join also needs the party to be exactly
// the arena team's size and the season to be enabled. A refusal is not an error here:
// it shows up as no WAIT_QUEUE, and WaitBattlefieldStatus names the reason.
func (b *ScenarioBot) JoinRatedArena(t *testing.T, battlemasterGUID uint64, arenaSlot client.ArenaSlot) {
	t.Helper()
	if err := b.World.JoinArenaQueue(battlemasterGUID, arenaSlot, true, true); err != nil {
		HarnessFailf(t, "%s: CMSG_BATTLEMASTER_JOIN_ARENA: %v", b.Name, err)
	}
}

// LeaveBattlefieldQueue answers the bot's most recent queue status with "leave queue",
// and reports whether there was one to answer.
//
// This is the only removal that reaches a group that has already been invited, which
// disbanding its arena team does not. The realm answers for the whole group, so one member
// is enough. Best effort: a bot that never queued is a no-op.
//
// Prefer DrainArenaQueueOnCleanup, which also waits for the realm to confirm the removal.
func (b *ScenarioBot) LeaveBattlefieldQueue(t *testing.T) bool {
	t.Helper()
	st, ok := b.lastQueuedStatus()
	if !ok {
		return false
	}
	if err := b.World.LeaveBattlefieldQueue(st); err != nil {
		t.Logf("%s: leave battlefield queue: %v", b.Name, err)
		return false
	}
	return true
}

// lastQueuedStatus returns the newest status of a queue this bot is still in, which is the
// one it can answer. A bot that never queued has none.
func (b *ScenarioBot) lastQueuedStatus() (client.BattlefieldStatus, bool) {
	return lastQueued(b.World.BattlefieldStatuses())
}

// lastQueued is lastQueuedStatus over statuses in the order they were received. A queue slot
// the realm has since answered with STATUS_NONE is over.
func lastQueued(statuses []client.BattlefieldStatus) (client.BattlefieldStatus, bool) {
	left := make(map[uint32]bool)
	for i := len(statuses) - 1; i >= 0; i-- {
		st := statuses[i]
		if !st.HasBattleground {
			left[st.QueueSlot] = true
			continue
		}
		if !left[st.QueueSlot] {
			return st, true
		}
	}
	return client.BattlefieldStatus{}, false
}

// EnableArenaSeason turns the arena season on and spawns the Arena Battlemasters, which
// are gated behind a game event that is off by default. Rated joins are refused outright
// without both.
//
// Cleanup puts the season back into the state it was found in and stops the event again.
// Stopping is the right restore because the event is GM-only on stock data: its row has
// start_time == end_time, and CheckOneGameEvent wants now < End, so it never turns itself
// on. Both are realm-global, so two tests that call this must not overlap.
func EnableArenaSeason(t *testing.T, bot *ScenarioBot) {
	t.Helper()

	var previousState int
	if err := bot.CharDB.QueryRow("SELECT season_state FROM active_arena_season").Scan(&previousState); err != nil {
		HarnessFailf(t, "read active_arena_season: %v", err)
	}

	bot.GM(t, ".arena season set state 1")
	bot.GM(t, fmt.Sprintf(".event start %d", ArenaTournamentEvent))
	t.Cleanup(func() {
		gmOnCleanup(t, bot, fmt.Sprintf(".event stop %d", ArenaTournamentEvent))
		if previousState != 1 {
			gmOnCleanup(t, bot, fmt.Sprintf(".arena season set state %d", previousState))
		}
	})
	bot.FlushWorld(t)
}

// gmOnCleanup runs a GM command from a cleanup and waits for the realm's answer to it, since
// one sent just before the session closes is dropped with it. cmd must be one the realm
// answers with a single system message. Best effort: it logs instead of failing.
func gmOnCleanup(t *testing.T, bot *ScenarioBot, cmd string) {
	answered := make(chan struct{}, 1)
	cancel := bot.World.AddPacketHook(func(op uint16, _ []byte) {
		if op != client.SmsgMessageChat {
			return
		}
		select {
		case answered <- struct{}{}:
		default:
		}
	})
	defer cancel()

	if err := bot.World.SendGMCommand(cmd); err != nil {
		t.Logf("%s: %s: %v", bot.Name, cmd, err)
		return
	}
	select {
	case <-answered:
	case <-time.After(arenaQueueLeaveTimeout):
		t.Logf("%s: no answer to %q within %s, so it may not have run",
			bot.Name, cmd, arenaQueueLeaveTimeout)
	}
}

// CreateArenaTeam creates an arena team captained by leader and brings member into it,
// returning the team id. On cleanup both are taken out of any queue they are still in, as
// by DrainArenaQueueOnCleanup, and then the team is disbanded.
//
// It builds a two player team, so the only teamType a rated queue will accept from it is
// client.ArenaTeam2v2: a rated join needs the party to be exactly the team's size, and a
// 3v3 or 5v5 team created here can never reach that. member must be at MaxPlayerLevel or
// the invite comes back as ERR_ARENA_TEAM_TARGET_TOO_LOW_S and the team stays at one.
//
// The name is a charter name, so it is capped at MAX_CHARTER_NAME and must be unique on
// the realm (UniqueArenaTeamName).
func CreateArenaTeam(t *testing.T, leader, member *ScenarioBot, name string, teamType client.ArenaTeamType) uint32 {
	t.Helper()

	leader.GM(t, fmt.Sprintf(`.arena create %s "%s" %d`, leader.Name, name, teamType))
	leader.FlushWorld(t)

	teamID := ArenaTeamIDByName(t, leader.CharDB, name)
	t.Cleanup(func() {
		gmOnCleanup(t, leader, fmt.Sprintf(".arena disband %d", teamID))
	})
	// Registered after the disband, so it runs before it.
	DrainArenaQueueOnCleanup(t, []*ScenarioBot{leader, member})

	if err := leader.World.InviteToArenaTeam(teamID, member.Name); err != nil {
		HarnessFailf(t, "CMSG_ARENA_TEAM_INVITE %s -> %s: %v", leader.Name, member.Name, err)
	}
	// Flush the inviter, not the invitee: FlushWorld only proves the commands queued on
	// the session it is called on have been applied. An accept that arrives before the
	// invite finds no pending invite and is dropped, and nothing here resends it.
	leader.FlushWorld(t)
	if err := member.World.AcceptArenaTeamInvite(); err != nil {
		HarnessFailf(t, "CMSG_ARENA_TEAM_ACCEPT %s: %v", member.Name, err)
	}
	member.FlushWorld(t)

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if ArenaTeamMemberCount(t, leader.CharDB, teamID) >= 2 {
			t.Logf("arena team %q id=%d captain=%s member=%s", name, teamID, leader.Name, member.Name)
			return teamID
		}
		time.Sleep(100 * time.Millisecond)
	}
	Preconditionf(t, "arena team %q (id=%d) never reached 2 members", name, teamID)
	return teamID
}

// ArenaTeamIDByName polls for the team row. ArenaTeam::Create persists through an async
// CharacterDatabase.Execute, so the row lands some time after the command is answered.
func ArenaTeamIDByName(t *testing.T, charDB *sql.DB, name string) uint32 {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		var id uint32
		err := charDB.QueryRow("SELECT arenaTeamId FROM arena_team WHERE name = ?", name).Scan(&id)
		if err == nil {
			return id
		}
		if err != sql.ErrNoRows {
			HarnessFailf(t, "query arena team %q: %v", name, err)
		}
		if time.Now().After(deadline) {
			Preconditionf(t, "arena team %q never appeared after `.arena create`", name)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// ArenaTeamMemberCount counts persisted members of a team.
func ArenaTeamMemberCount(t *testing.T, charDB *sql.DB, teamID uint32) int {
	t.Helper()
	var n int
	if err := charDB.QueryRow(
		"SELECT COUNT(*) FROM arena_team_member WHERE arenaTeamId = ?", teamID).Scan(&n); err != nil {
		HarnessFailf(t, "count arena team members (team %d): %v", teamID, err)
	}
	return n
}

// ArenaTeamRating reads a team's persisted rating. The row is written with the rating
// at creation, so once ArenaTeamIDByName has found it there is nothing to wait for.
//
// The rating comes from the ArenaTeam constructor, not from AddMember, and which config
// it reads depends on the active season: Arena.LegacyArenaStartRating (1500) while
// active_arena_season.season_id is below 6, Arena.ArenaStartRating (0) from season 6 on.
// So zero is a valid answer on a modern season and the wrong expectation on an early one.
//
// Matchmaker rating is not readable here: AddMember seeds it from
// Arena.ArenaStartMatchmakerRating, but it only reaches the DB through
// ArenaTeam::SaveToDB after a match.
func ArenaTeamRating(t *testing.T, charDB *sql.DB, teamID uint32) uint32 {
	t.Helper()
	var rating uint32
	if err := charDB.QueryRow(
		"SELECT rating FROM arena_team WHERE arenaTeamId = ?", teamID).Scan(&rating); err != nil {
		HarnessFailf(t, "read arena team %d rating: %v", teamID, err)
	}
	return rating
}

// ArenaSlotForTeamType maps an arena team type to the slot the DB and the queue index by
// (2v2 -> 0, 3v3 -> 1, 5v5 -> 2). A module can remap this, so a realm running one is out
// of scope here.
func ArenaSlotForTeamType(t *testing.T, teamType client.ArenaTeamType) client.ArenaSlot {
	t.Helper()
	switch teamType {
	case client.ArenaTeam2v2:
		return client.ArenaSlot2v2
	case client.ArenaTeam3v3:
		return client.ArenaSlot3v3
	case client.ArenaTeam5v5:
		return client.ArenaSlot5v5
	}
	HarnessFailf(t, "no arena slot for team type %d", teamType)
	return 0
}

// SeedMatchmakerRating fixes the matchmaker rating a character brings into an arena team.
//
// This is the only lever on it. No GM command sets a team's rating, and writing arena_team
// does not reach a team the realm already has loaded. The seeded row needs no config change
// and no reload.
//
// Seed every member BEFORE CreateArenaTeam. The rating is read once, as the member joins,
// and never again. It takes the same team type as CreateArenaTeam so the two cannot
// disagree about which bracket is being set up.
//
// Give both members of one team the same rating: the queue pairs on the team average of
// the online party members. The rating must lie within MinSeedableRating and
// MaxSeedableRating.
//
// The row is removed on cleanup, since it is keyed on the character guid alone and would
// otherwise follow that guid to its next owner.
//
// A seed that did not apply is caught here rather than downstream, where nothing can see
// it: it leaves every team on the config default, the same rating for all of them, which a
// caller comparing the ratings it intended would read as a real spread. So the write is
// read back, and seeding a character already on a team for this bracket is refused rather
// than silently having no effect.
func SeedMatchmakerRating(t *testing.T, bot *ScenarioBot, teamType client.ArenaTeamType, rating int) {
	t.Helper()
	slot := ArenaSlotForTeamType(t, teamType)

	// Not covered by the read-back below, which only reads back matchMakerRating.
	if rating < MinSeedableRating || rating > MaxSeedableRating {
		HarnessFailf(t, "matchmaker rating %d for %s is outside %d to %d, which is all "+
			"character_arena_stats can hold and read back", rating, bot.Name,
			MinSeedableRating, MaxSeedableRating)
	}

	var joined int
	if err := bot.CharDB.QueryRow(
		`SELECT COUNT(*) FROM arena_team_member m JOIN arena_team t ON t.arenaTeamId = m.arenaTeamId
		 WHERE m.guid = ? AND t.type = ?`, bot.GUID, uint8(teamType)).Scan(&joined); err != nil {
		HarnessFailf(t, "check %s for an existing %dv%d team: %v", bot.Name, teamType, teamType, err)
	}
	if joined > 0 {
		HarnessFailf(t, "%s is already on a %dv%d team, so seeding now cannot change its "+
			"matchmaker rating: AddMember read character_arena_stats when the team was formed. "+
			"Seed before CreateArenaTeam.", bot.Name, teamType, teamType)
	}

	if _, err := bot.CharDB.Exec(
		`REPLACE INTO character_arena_stats (guid, slot, matchMakerRating, maxMMR) VALUES (?, ?, ?, ?)`,
		bot.GUID, uint8(slot), rating, rating); err != nil {
		HarnessFailf(t, "seed matchmaker rating %d for %s (guid %d, slot %d): %v",
			rating, bot.Name, bot.GUID, slot, err)
	}
	t.Cleanup(func() {
		if _, err := bot.CharDB.Exec(
			`DELETE FROM character_arena_stats WHERE guid = ? AND slot = ?`,
			bot.GUID, uint8(slot)); err != nil {
			t.Logf("clear matchmaker rating for %s: %v", bot.Name, err)
		}
	})

	if got := MatchmakerRating(t, bot.CharDB, bot.GUID, teamType); got != rating {
		HarnessFailf(t, "seeded matchmaker rating %d for %s (guid %d, slot %d) but read back %d",
			rating, bot.Name, bot.GUID, slot, got)
	}
	t.Logf("seeded matchmaker rating %d for %s (guid %d, slot %d)", rating, bot.Name, bot.GUID, slot)
}

// MatchmakerRating reads the seeded character_arena_stats rating, or 0 when the character
// has no row and the realm would fall back to Arena.ArenaStartMatchmakerRating.
func MatchmakerRating(t *testing.T, charDB *sql.DB, charGUID uint64, teamType client.ArenaTeamType) int {
	t.Helper()
	slot := ArenaSlotForTeamType(t, teamType)

	var rating int
	err := charDB.QueryRow(
		`SELECT matchMakerRating FROM character_arena_stats WHERE guid = ? AND slot = ?`,
		charGUID, uint8(slot)).Scan(&rating)
	if err == sql.ErrNoRows {
		return 0
	}
	if err != nil {
		HarnessFailf(t, "read matchmaker rating (guid %d, slot %d): %v", charGUID, slot, err)
	}
	return rating
}

// DrainArenaQueueOnCleanup takes every bot out of its bracket when the test ends, waits for
// the realm to confirm it, and clears what declining an invite wrote.
//
// A bot left queued keeps the rated bracket occupied until its invite expires, and the next
// test pairs against it. Disbanding the team does not cover a team that has already been
// invited, so the invite is answered with "leave queue" instead, and the realm's STATUS_NONE
// answer is waited for rather than assumed. Earlier statuses are dropped first so an older
// one cannot be read as that answer.
//
// Declining a rated invite is counted as a loss, which writes a character_arena_stats row
// asynchronously. The row is keyed on the character guid alone and outlives the character,
// so it is cleared here, in two passes because the write can land after the first.
//
// CreateArenaTeam registers this for its own two members, so only a bot queued some other
// way needs it.
func DrainArenaQueueOnCleanup(t *testing.T, bots []*ScenarioBot) {
	t.Helper()
	t.Cleanup(func() {
		var queued []*ScenarioBot
		var statuses []client.BattlefieldStatus
		for _, b := range bots {
			if st, ok := b.lastQueuedStatus(); ok {
				queued = append(queued, b)
				statuses = append(statuses, st)
			}
		}

		// Drop before answering any of them: a bot can be confirmed by its team mate's
		// leave rather than by its own.
		for _, b := range queued {
			b.DrainBattlefieldStatuses()
		}
		for i, b := range queued {
			if err := b.World.LeaveBattlefieldQueue(statuses[i]); err != nil {
				t.Logf("%s: leave battlefield queue: %v", b.Name, err)
			}
		}

		_, left := TryWaitBattlefieldStatusEach(
			queued, client.BattlegroundStatusNone, arenaQueueLeaveTimeout)
		for i, b := range queued {
			if !left[i] {
				t.Logf("%s: no STATUS_NONE within %s, so the bracket may still hold it",
					b.Name, arenaQueueLeaveTimeout)
			}
		}

		clearArenaStats(t, bots)
		time.Sleep(arenaStatsSettleDelay)
		clearArenaStats(t, bots)
	})
}

// clearArenaStats removes every bot's character_arena_stats rows. Best effort: it logs
// instead of failing, because a cleanup that fails the test skips the cleanups queued
// before it.
func clearArenaStats(t *testing.T, bots []*ScenarioBot) {
	for _, b := range bots {
		if _, err := b.CharDB.Exec(
			"DELETE FROM character_arena_stats WHERE guid = ?", b.GUID); err != nil {
			t.Logf("clear arena stats for %s (guid %d): %v", b.Name, b.GUID, err)
		}
	}
}

// TeleportToArenaBattlemaster places every bot at one Arena Battlemaster spawn and
// returns that unit's live ObjectGuid, which is the same for every bot.
//
// The spawn is looked up in the world DB rather than hardcoded, and one specific spawn
// is targeted rather than `.go creature <entry>`, which picks an arbitrary one that may
// not be in a loaded grid. Requires EnableArenaSeason first: the spawns are gated behind
// a game event.
func TeleportToArenaBattlemaster(t *testing.T, bots ...*ScenarioBot) uint64 {
	t.Helper()
	if len(bots) == 0 {
		HarnessFailf(t, "TeleportToArenaBattlemaster: no bots")
	}

	spawnGUID := ArenaBattlemasterSpawn(t, bots[0])
	t.Logf("using Arena Battlemaster spawn guid=%d", spawnGUID)

	for _, b := range bots {
		GoCreatureGUID(t, b.World, spawnGUID)
		b.FlushWorld(t)
	}
	return WaitNearbyUnitByEntry(t, bots[0].World, ArenaBattlemasterEntry, 20*time.Second)
}

// ArenaBattlemasterSpawn returns a continent Arena Battlemaster spawn id from the world DB.
func ArenaBattlemasterSpawn(t *testing.T, bot *ScenarioBot) uint32 {
	t.Helper()
	db := bot.withWorldDB(t)

	var guid uint32
	if err := db.QueryRow(
		"SELECT guid FROM creature WHERE id = ? AND map = 0 ORDER BY guid LIMIT 1",
		ArenaBattlemasterEntry).Scan(&guid); err != nil {
		Preconditionf(t, "no Arena Battlemaster (entry %d) spawned on map 0: %v", ArenaBattlemasterEntry, err)
	}
	return guid
}
