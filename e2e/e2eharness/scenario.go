package e2eharness

import (
	"database/sql"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// 3.3.5a race / class IDs used by character create.
const (
	RaceHuman    uint8 = 1
	RaceOrc      uint8 = 2
	RaceDwarf    uint8 = 3
	RaceNightElf uint8 = 4
	RaceUndead   uint8 = 5
	RaceTauren   uint8 = 6
	RaceGnome    uint8 = 7
	RaceTroll    uint8 = 8
	RaceBloodElf uint8 = 10
	RaceDraenei  uint8 = 11

	ClassWarrior     uint8 = 1
	ClassPaladin     uint8 = 2
	ClassHunter      uint8 = 3
	ClassRogue       uint8 = 4
	ClassPriest      uint8 = 5
	ClassDeathKnight uint8 = 6
	ClassShaman      uint8 = 7
	ClassMage        uint8 = 8
	ClassWarlock     uint8 = 9
	ClassDruid       uint8 = 11
)

// Spell / item / quest constants referenced by AC-issue scenario tests.
const (
	// Spells
	SpellCharge               = 100   // Charge (warrior)
	SpellIntercept            = 20252 // Intercept
	SpellBattleStance         = 2457
	SpellSweepingStrikes      = 12328
	SpellExecute              = 5308
	SpellRaiseDead            = 46584 // DK Raise Dead
	SpellBloodStrike          = 45902
	SpellBloodTap             = 45529
	SpellGroundingTotem       = 8177
	SpellGroundingTotemEffect = 8178 // aura on the totem (consumed wrongly by AoE)
	SpellRainOfFire           = 5740
	SpellBlendingInAura       = 45614 // Imbued Scourge Shroud effect
	SpellSummonTargetDummy    = 4071  // Target Dummy (engineering item spell)
	SpellSummonAdvDummy       = 4072  // Advanced Target Dummy
	SpellSummonMasterDummy    = 19805 // Masterwork Target Dummy
	SpellMountSwiftGryphon    = 32235 // Swift Blue Gryphon (flying)

	// Items
	ItemImbuedScourgeShroud = 34782
	ItemTargetDummy         = 4366
	ItemAdvTargetDummy      = 4392
	ItemMasterTargetDummy   = 16023
	ItemCorpseDust          = 37201 // Raise Dead reagent

	// Creatures
	CreatureTargetDummy       = 2673
	CreatureAdvTargetDummy    = 2674
	CreatureMasterTargetDummy = 12426
	CreatureKologarn          = 32930
	CreatureYorusBarleybrew   = 6166 // Rethban Gauntlet related
	CreatureGroundingTotem    = 5925

	// Quests
	QuestRethbanGauntlet = 1699  // QUEST_FLAGS_STAY_ALIVE (0x1)
	QuestBlendingIn      = 11633 // Borean Tundra cloak quest
	QuestWasteNotWantNot = 10055

	// Quest status (character_queststatus.status)
	QuestStatusNone       uint8 = 0
	QuestStatusComplete   uint8 = 1
	QuestStatusIncomplete uint8 = 3
	QuestStatusFailed     uint8 = 5

	// Maps / positions from AC issues
	MapEasternKingdoms = 0
	MapOutland         = 530
	MapNorthrend       = 571
	MapUlduar          = 603
)

// UnitHasAura reports whether a tracked unit currently has spellID as an aura.
func UnitHasAura(w *client.WorldClient, guid uint64, spellID uint32) bool {
	obj := w.GetObject(guid)
	if obj == nil {
		return false
	}
	return obj.HasAura(spellID)
}

// UnitTargetGUID reads UNIT_FIELD_TARGET (2×uint32) from a tracked unit, or 0.
func UnitTargetGUID(w *client.WorldClient, guid uint64) uint64 {
	obj := w.GetObject(guid)
	if obj == nil {
		return 0
	}
	low := obj.Value(client.UnitFieldTarget)
	high := obj.Value(client.UnitFieldTarget + 1)
	return uint64(low) | (uint64(high) << 32)
}

// UnitInCombat reports UNIT_FLAG_IN_COMBAT on a tracked unit.
func UnitInCombat(w *client.WorldClient, guid uint64) bool {
	obj := w.GetObject(guid)
	if obj == nil {
		return false
	}
	return obj.Value(client.UnitFieldFlags)&client.UnitFlagInCombat != 0
}

// WaitUnitAura waits until UnitHasAura is true (or fails the test).
func WaitUnitAura(t *testing.T, w *client.WorldClient, guid uint64, spellID uint32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if UnitHasAura(w, guid, spellID) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("unit 0x%X missing aura %d within %s", guid, spellID, timeout)
}

// SessionAlive reports whether the world session is still usable (socket open).
func SessionAlive(s *Session) bool {
	if s == nil || s.World == nil {
		return false
	}
	// InWorld is false after disconnect; also phase may stick.
	return s.World.IsInWorld() || s.World.SessionPhase() != 0
}

// ScenarioBot is a single logged-in bot prepared for a scenario test.
// Prefer NewScenario / NewSolo over ad-hoc LoginBot calls.
// Methods on ScenarioBot (Teleport, Cast, Die, …) hide World vs Session choice.
type ScenarioBot struct {
	*Session
	AuthDB *sql.DB
	CharDB *sql.DB
	Ident  BotIdent
	Role   string // optional label from BotSpec.Role
}

// BotSpec describes one bot inside a heterogeneous multi-bot scenario.
// Zero Race/Class default to Human/Warrior; zero Level skips leveling.
type BotSpec struct {
	Role          string // optional label (e.g. "shaman", "victim")
	Race          uint8
	Class         uint8
	Level         int
	LearnAllClass bool
}

// ScenarioOpts configures a multi-bot scenario fixture.
//
// Prefer Bots for multi-role scenarios (different race/class per bot).
// Count/Race/Class/Level/LearnAllClass apply when Bots is empty (homogeneous).
type ScenarioOpts struct {
	// Prefix for unique account names (short, letters preferred).
	Prefix string
	// Bots, when non-empty, defines each bot individually (overrides Count/Race/Class).
	Bots []BotSpec
	// Count of bots when Bots is empty (default 1).
	Count int
	// Race/Class apply to all homogeneous bots when non-zero (default Human Warrior).
	Race  uint8
	Class uint8
	// Level applied via GM after login when > 0 (homogeneous only; use BotSpec.Level otherwise).
	Level int
	// LearnAllClass runs `.learn all my class` when true (homogeneous only).
	LearnAllClass bool
	// SkipGM leaves GM mode off (default is GM on — needed for .go/.learn/.quest).
	SkipGM bool
	// CombatReady runs CombatReadyDefaults after login (gm off + god).
	// Use for fight tests; leave false when you need GM mode for setup first.
	CombatReady bool
	// CombatReadyFull runs CombatReady with god+power after login.
	CombatReadyFull bool
	// StartPad, when non-nil, teleports every bot there after setup.
	StartPad *Position3
}

// NewScenario opens DBs, creates accounts, logs bots in, and applies setup.
// Sessions and DBs are closed via t.Cleanup.
func NewScenario(t *testing.T, opt ScenarioOpts) []*ScenarioBot {
	t.Helper()
	if opt.Prefix == "" {
		opt.Prefix = "Sc"
	}
	enableGM := !opt.SkipGM

	var specs []BotSpec
	if len(opt.Bots) > 0 {
		specs = opt.Bots
	} else {
		n := opt.Count
		if n <= 0 {
			n = 1
		}
		race, class := opt.Race, opt.Class
		if race == 0 {
			race = RaceHuman
		}
		if class == 0 {
			class = ClassWarrior
		}
		specs = make([]BotSpec, n)
		for i := range specs {
			specs[i] = BotSpec{
				Race: race, Class: class,
				Level: opt.Level, LearnAllClass: opt.LearnAllClass,
			}
		}
	}

	authDB, charDB := OpenTestDBs(t)
	idents := make([]BotIdent, len(specs))
	// Build unique idents then stamp race/class from specs.
	base := MakeBotIdents(opt.Prefix, len(specs))
	for i, sp := range specs {
		idents[i] = base[i]
		r, c := sp.Race, sp.Class
		if r == 0 {
			r = RaceHuman
		}
		if c == 0 {
			c = ClassWarrior
		}
		idents[i].Race = r
		idents[i].Class = c
	}
	EnsureBotAccounts(t, authDB, idents)
	sessions := LoginBots(t, idents)

	bots := make([]*ScenarioBot, len(sessions))
	for i, s := range sessions {
		sp := specs[i]
		bots[i] = &ScenarioBot{
			Session: s,
			AuthDB:  authDB,
			CharDB:  charDB,
			Ident:   idents[i],
			Role:    sp.Role,
		}
		if enableGM {
			EnableGM(t, s.World)
		}
		if sp.Level > 0 {
			SetLevel(t, s.World, sp.Level)
		}
		if sp.LearnAllClass {
			LearnAllMyClass(t, s.World)
		}
		if opt.CombatReadyFull {
			CombatReady(t, s.World, CombatReadyOpts{God: true, Power: true})
		} else if opt.CombatReady {
			CombatReadyDefaults(t, s.World)
		}
		if opt.StartPad != nil {
			TeleportPad(t, s.World, *opt.StartPad)
		}
	}
	return bots
}

// NewSolo is NewScenario with a single bot; returns that bot.
// If opt.Bots is set, only Bots[0] is used.
func NewSolo(t *testing.T, opt ScenarioOpts) *ScenarioBot {
	t.Helper()
	if len(opt.Bots) > 1 {
		opt.Bots = opt.Bots[:1]
	}
	if len(opt.Bots) == 0 {
		opt.Count = 1
	}
	return NewScenario(t, opt)[0]
}

// ByRole returns the first bot whose Role matches, or fatals.
func ByRole(t *testing.T, bots []*ScenarioBot, role string) *ScenarioBot {
	t.Helper()
	for _, b := range bots {
		if b.Role == role {
			return b
		}
	}
	t.Fatalf("no bot with role %q", role)
	return nil
}

// MakeBotIdentsRaceClass is MakeBotIdents with race/class on each identity.
func MakeBotIdentsRaceClass(prefix string, n int, race, class uint8) []BotIdent {
	out := MakeBotIdents(prefix, n)
	for i := range out {
		out[i].Race = race
		out[i].Class = class
	}
	return out
}

// LoginBots logs in each identity using its Race/Class (defaults Human/Warrior).
// Sessions are registered with t.Cleanup(Close).
func LoginBots(t *testing.T, idents []BotIdent) []*Session {
	t.Helper()
	return loginBots(t, idents, true)
}

func loginBots(t *testing.T, idents []BotIdent, autoCleanup bool) []*Session {
	t.Helper()
	if len(idents) == 0 {
		return nil
	}
	// Reuse parallel path for multi-bot; inject race/class via loginBotWithRetryRaceClass.
	if len(idents) == 1 {
		s, err := loginBotWithRetryRaceClass(t, idents[0])
		if err != nil {
			t.Fatalf("login %s: %v", idents[0].Account, err)
		}
		if autoCleanup {
			t.Cleanup(func() { s.Close() })
		}
		return []*Session{s}
	}

	// Parallel multi-bot login (same as LoginAllianceBots but race/class aware).
	sessions := make([]*Session, len(idents))
	errs := make([]error, len(idents))
	sem := make(chan struct{}, MaxParallelLogins)
	type result struct {
		i int
		s *Session
		e error
	}
	// sequential-safe: use goroutines like loginAllianceBots
	done := make(chan result, len(idents))
	start := time.Now()
	t.Logf("parallel login: %d bots (max concurrency %d)", len(idents), MaxParallelLogins)
	for i, id := range idents {
		i, id := i, id
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			s, err := loginBotWithRetryRaceClass(t, id)
			done <- result{i: i, s: s, e: err}
		}()
	}
	for range idents {
		r := <-done
		if r.e != nil {
			errs[r.i] = r.e
			continue
		}
		sessions[r.i] = r.s
	}
	var firstErr error
	var firstIdx int
	for i, err := range errs {
		if err != nil {
			firstErr = err
			firstIdx = i
			break
		}
	}
	if firstErr != nil {
		for _, s := range sessions {
			if s != nil {
				s.Close()
			}
		}
		t.Fatalf("login %s: %v", idents[firstIdx].Account, firstErr)
	}
	t.Logf("parallel login: %d bots ready in %s", len(idents), time.Since(start).Round(time.Millisecond))
	if autoCleanup {
		for _, s := range sessions {
			s := s
			t.Cleanup(func() { s.Close() })
		}
	}
	return sessions
}

func loginBotWithRetryRaceClass(t *testing.T, id BotIdent) (*Session, error) {
	t.Helper()
	race, class := id.Race, id.Class
	if race == 0 {
		race = RaceHuman
	}
	if class == 0 {
		class = ClassWarrior
	}
	var s *Session
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		s, err = LoginBot(t, LoginOptions{
			User:     id.Account,
			Password: DefaultPassword,
			CharName: id.CharName,
			Race:     race,
			Class:    class,
		})
		if err == nil {
			return s, nil
		}
		t.Logf("login %s attempt %d failed: %v", id.Account, attempt, err)
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
	}
	return nil, err
}

// SetLevel sets character level via GM `.character level N` (absolute).
// Falls back to `.levelup` delta if the absolute command is unavailable.
func SetLevel(t *testing.T, w *client.WorldClient, level int) {
	t.Helper()
	if level <= 0 {
		return
	}
	if guid := w.CharGUID(); guid != 0 {
		_ = w.SetTarget(guid)
	}
	cur := int(w.PlayerLevel())
	if cur == 0 {
		cur = 1
	}
	if level == cur {
		return
	}
	// Preferred: absolute set (works regardless of current level).
	MustGM(t, w, fmt.Sprintf(".character level %d", level))
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if int(w.PlayerLevel()) >= level {
			t.Logf("level set to %d", w.PlayerLevel())
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Fallback: relative levelup from observed current.
	cur = int(w.PlayerLevel())
	if cur == 0 {
		cur = 1
	}
	if level > cur {
		MustGM(t, w, fmt.Sprintf(".levelup %d", level-cur))
		deadline = time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if int(w.PlayerLevel()) >= level {
				t.Logf("level set to %d via levelup", w.PlayerLevel())
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	t.Logf("WARNING: level still %d after character level/levelup toward %d (continuing)", w.PlayerLevel(), level)
}

// LearnAllMyClass runs `.learn all my class` (class spells + talents ranks available).
func LearnAllMyClass(t *testing.T, w *client.WorldClient) {
	t.Helper()
	MustGM(t, w, ".learn all my class")
	// Spells arrive via SMSG_LEARNED_SPELL / re-send; small settle for known-spells map.
	// Prefer polling KnowsSpell for a signature class spell when callers care.
	time.Sleep(200 * time.Millisecond)
}

// LearnSpell learns a single spell by ID via GM and waits until KnowsSpell.
func LearnSpell(t *testing.T, w *client.WorldClient, spellID uint32) {
	t.Helper()
	if w.KnowsSpell(spellID) {
		return
	}
	MustGM(t, w, fmt.Sprintf(".learn %d", spellID))
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if w.KnowsSpell(spellID) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	// Some cores don't re-send initial spells for GM .learn; allow cast path anyway.
	t.Logf("WARNING: KnowsSpell(%d) still false after .learn (continuing)", spellID)
}

// SpawnNPCAndWait spawns a temp creature and waits for its live GUID in the object cache.
func SpawnNPCAndWait(t *testing.T, w *client.WorldClient, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	SpawnNPC(t, w, entry)
	return WaitNearbyUnitByEntry(t, w, entry, timeout)
}

// AddQuest adds a quest to the player's log via GM `.quest add`.
func AddQuest(t *testing.T, w *client.WorldClient, questID uint32) {
	t.Helper()
	MustGM(t, w, fmt.Sprintf(".quest add %d", questID))
}

// Die kills the player via GM `.die` (requires target self).
func Die(t *testing.T, w *client.WorldClient) {
	t.Helper()
	if guid := w.CharGUID(); guid != 0 {
		_ = w.SetTarget(guid)
	}
	MustGM(t, w, ".die")
}

// WaitDead polls until player health is 0 (or timeout).
func WaitDead(t *testing.T, w *client.WorldClient, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w.Health() == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("player still alive (hp=%d/%d) after %s", w.Health(), w.MaxHealth(), timeout)
}

// ReleaseSpirit sends CMSG_REPOP_REQUEST after death.
func ReleaseSpirit(t *testing.T, w *client.WorldClient) {
	t.Helper()
	if err := w.RepopRequest(); err != nil {
		t.Fatalf("repop: %v", err)
	}
}

// WaitAura waits until SelfHasAura(spellID) is true.
func WaitAura(t *testing.T, w *client.WorldClient, spellID uint32, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if w.SelfHasAura(spellID) {
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	t.Fatalf("aura %d not present within %s (auras=%v)", spellID, timeout, w.SelfAuras())
}

// WaitAuraGone waits until SelfHasAura(spellID) is false.
func WaitAuraGone(t *testing.T, w *client.WorldClient, spellID uint32, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !w.SelfHasAura(spellID) {
			return true
		}
		time.Sleep(40 * time.Millisecond)
	}
	return false
}

// AuraPresent is a non-fatal snapshot check.
func AuraPresent(w *client.WorldClient, spellID uint32) bool {
	return w.SelfHasAura(spellID)
}

// ApplyAura applies an aura via GM `.aura <spell>` and waits until SelfHasAura
// reports it (requires correct SMSG_AURA_UPDATE parsing).
func ApplyAura(t *testing.T, w *client.WorldClient, spellID uint32) {
	t.Helper()
	if guid := w.CharGUID(); guid != 0 {
		_ = w.SetTarget(guid)
	}
	MustGM(t, w, fmt.Sprintf(".aura %d", spellID))
	// Fallback path used by some cores: cast the spell on self.
	if !w.SelfHasAura(spellID) {
		MustGM(t, w, fmt.Sprintf(".cast self %d", spellID))
	}
	WaitAura(t, w, spellID, 6*time.Second)
}

// CheatPower enables infinite power (rage/energy/mana) via `.cheat power on`.
func CheatPower(t *testing.T, w *client.WorldClient) {
	t.Helper()
	MustGM(t, w, ".cheat power on")
}

// CheatGod enables god mode via `.cheat god on`.
func CheatGod(t *testing.T, w *client.WorldClient) {
	t.Helper()
	MustGM(t, w, ".cheat god on")
}

// GiveShamanTotems adds the four basic totem items required to cast totems.
func GiveShamanTotems(t *testing.T, w *client.WorldClient) {
	t.Helper()
	// Earth / Fire / Water / Air totems (classic totem tools).
	for _, entry := range []uint32{5175, 5176, 5177, 5178} {
		AddItem(t, w, entry, 1)
	}
}

// SaveCharacter forces a DB flush via `.save` so quest/char queries see live state.
func SaveCharacter(t *testing.T, w *client.WorldClient) {
	t.Helper()
	MustGM(t, w, ".save")
	// Disk write is async; short settle before SQL.
	time.Sleep(300 * time.Millisecond)
}

// TeleportXYZ is TeleportGo with a clearer scenario name.
func TeleportXYZ(t *testing.T, w *client.WorldClient, x, y, z float32, mapID uint32) {
	t.Helper()
	TeleportGo(t, w, x, y, z, mapID)
}

// SpawnNPC spawns a temporary creature via `.npc add temp <entry>`.
func SpawnNPC(t *testing.T, w *client.WorldClient, entry uint32) {
	t.Helper()
	MustGM(t, w, fmt.Sprintf(".npc add temp %d", entry))
}

// WaitNearbyUnitEntry waits for a live unit with the given template entry.
// Deprecated: prefer ScenarioBot.WaitUnit.
func WaitNearbyUnitEntry(t *testing.T, w *client.WorldClient, entry uint32, timeout time.Duration) uint64 {
	t.Helper()
	return WaitNearbyUnitByEntry(t, w, entry, timeout)
}

// Position returns current player position.
func Position(w *client.WorldClient) (x, y, z float32, mapID uint32) {
	x, y, z, _, mapID = w.Position()
	return x, y, z, mapID
}

// Distance3D is Euclidean distance between two points.
func Distance3D(x1, y1, z1, x2, y2, z2 float32) float32 {
	dx := float64(x1 - x2)
	dy := float64(y1 - y2)
	dz := float64(z1 - z2)
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

// FaceUnit turns the player toward a tracked unit GUID (MSG_MOVE_SET_FACING).
func FaceUnit(t *testing.T, w *client.WorldClient, targetGUID uint64) {
	t.Helper()
	obj := w.GetObject(targetGUID)
	if obj == nil {
		t.Logf("FaceUnit: target 0x%X not in cache", targetGUID)
		return
	}
	px, py, _, _ := Position(w)
	dx := float64(obj.PosX - px)
	dy := float64(obj.PosY - py)
	o := float32(math.Atan2(dy, dx))
	if err := w.SetFacing(o); err != nil {
		t.Logf("SetFacing: %v", err)
	}
	_ = w.SetTarget(targetGUID)
}
