package e2eharness

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// Position3 is a map position used by pads and StartPad opts.
type Position3 struct {
	X, Y, Z float32
	Map     uint32
	// O is optional facing (radians); 0 is fine when unused.
	O float32
}

// NamedPad is a hand-picked isolation location for package-parallel e2e.
type NamedPad struct {
	Name string
	Pos  Position3
}

// IsolationPads are far-apart world locations so `go test ./...` can run packages
// in parallel without sharing one Stormwind AOI. Assigned stickily per suite folder.
//
// Source: operator-captured coords (SW towers, Elwynn house, Nagrand islands, Durotar peaks).
// Icecrown was incomplete in the capture dump — not included until coords are confirmed.
var IsolationPads = []NamedPad{
	{Name: "Tower1", Pos: Position3{X: -9110.266, Y: 470.96655, Z: 137.20119, O: 0.85138357, Map: MapEasternKingdoms}},
	{Name: "Tower2", Pos: Position3{X: -9043.385, Y: 376.87408, Z: 137.45674, O: 0.7139602, Map: MapEasternKingdoms}},
	{Name: "AbandonHouse", Pos: Position3{X: -9297.518, Y: 652.88934, Z: 131.09396, O: 4.582049, Map: MapEasternKingdoms}},
	{Name: "NagrandArena", Pos: Position3{X: -2048.0647, Y: 6647.59, Z: 13.057503, O: 3.3466177, Map: MapOutland}},
	{Name: "FloatingIsland1", Pos: Position3{X: -2140.973, Y: 7758.02, Z: 154.28343, O: 0.2717825, Map: MapOutland}},
	{Name: "FloatingIsland2", Pos: Position3{X: -2500.6123, Y: 8585.174, Z: 189.1715, O: 1.3281429, Map: MapOutland}},
	{Name: "FloatingIsland3", Pos: Position3{X: -3095.3198, Y: 8858.52, Z: -162.47665, O: 3.8885531, Map: MapOutland}},
	{Name: "InMountains1", Pos: Position3{X: 1626.4886, Y: -3638.4875, Z: 215.53114, O: 4.8191752, Map: MapKalimdor}},
	{Name: "InMountains2", Pos: Position3{X: 1153.9037, Y: -2586.8608, Z: 252.40727, O: 3.7353268, Map: MapKalimdor}},
	{Name: "InMountains3", Pos: Position3{X: -1945.4078, Y: -3252.7837, Z: 186.5974, O: 2.235215, Map: MapKalimdor}},
}

// PreferredPackagePads maps suite keys (relative to e2e/suites/ or "smoke") to pad names.
// Keep this 1:1 with IsolationPads for combat-heavy packages. Suites not listed get
// an unused pad if any remain, otherwise a stable hash share.
var PreferredPackagePads = map[string]string{
	"combat/threat":   "Tower1",
	"combat/death":    "Tower2",
	"combat/pets":     "AbandonHouse",
	"combat/charm":    "NagrandArena",
	"combat/vehicles": "FloatingIsland1",
	"social/loot":     "FloatingIsland2",
	"social/group":    "FloatingIsland3",
	"social/trade":    "InMountains1",
	"spells/cast":     "InMountains2",
	"spells/effects":  "InMountains3",
	// Combat-lite suites that still spawn/engage must not first-free onto Tower1 under
	// `go test -p N` (each package is its own process; "first free" → always Tower1).
	// They share via stable hash among non-preferred pads when possible; when every pad
	// has a preferred owner they hash-share the pool (see assignPackagePad).
}

// PadStormwindOutskirts is a legacy alias for AbandonHouse (Elwynn isolation pad).
// Prefer PackagePad(t) so parallel packages do not share one location.
var PadStormwindOutskirts = IsolationPads[2].Pos // AbandonHouse

// CombatPads is the list of isolation positions (for iteration / tests).
var CombatPads []Position3

func init() {
	CombatPads = make([]Position3, len(IsolationPads))
	for i, p := range IsolationPads {
		CombatPads[i] = p.Pos
	}
}

var (
	packagePadMu     sync.Mutex
	packagePadAssign = map[string]int{} // suiteKey -> IsolationPads index
)

// PackagePad returns the isolation pad for this test's package (suite folder).
// All tests in the same package share one pad for the process lifetime so multi-bot
// scenarios stay co-located; different packages get different pads when possible.
//
// Use with: go test ./... -parallel 1  (serial tests per package, parallel packages).
func PackagePad(t testing.TB) Position3 {
	t.Helper()
	key := suiteKeyFromCaller()
	idx, name := assignPackagePad(key)
	p := IsolationPads[idx].Pos
	t.Logf("PackagePad suite=%s pad=%s map=%d (%.1f,%.1f,%.1f)", key, name, p.Map, p.X, p.Y, p.Z)
	return p
}

// PadFor is an alias of PackagePad (package-level isolation). Prefer PackagePad for clarity.
func PadFor(t testing.TB) Position3 {
	t.Helper()
	return PackagePad(t)
}

func assignPackagePad(key string) (idx int, name string) {
	packagePadMu.Lock()
	defer packagePadMu.Unlock()
	if i, ok := packagePadAssign[key]; ok {
		return i, IsolationPads[i].Name
	}

	// Preferred combat/social suites always pin their pad — even across `go test -p N`
	// processes (each process has its own map, so "first free" would collapse every
	// unlisted suite onto Tower1 and thrash with combat/threat).
	if pref, ok := PreferredPackagePads[key]; ok {
		if i, ok := padIndexByName(pref); ok {
			packagePadAssign[key] = i
			return i, IsolationPads[i].Name
		}
	}

	// Unlisted suites: stable hash so different suite keys spread across pads when
	// packages run in parallel processes. Prefer pads not reserved by PreferredPackagePads
	// when any remain free in this process; otherwise hash over the full pool.
	used := make(map[int]bool, len(packagePadAssign))
	for _, i := range packagePadAssign {
		used[i] = true
	}
	reserved := preferredPadIndices()
	freeNonPref := make([]int, 0, len(IsolationPads))
	for i := range IsolationPads {
		if reserved[i] || used[i] {
			continue
		}
		freeNonPref = append(freeNonPref, i)
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	sum := h.Sum32()
	if len(freeNonPref) > 0 {
		i := freeNonPref[int(sum)%len(freeNonPref)]
		packagePadAssign[key] = i
		return i, IsolationPads[i].Name
	}
	// All pads preferred or in-use in this process: hash-share the full pool.
	i := int(sum) % len(IsolationPads)
	packagePadAssign[key] = i
	return i, IsolationPads[i].Name + "(shared)"
}

// preferredPadIndices returns pad indexes claimed by PreferredPackagePads.
func preferredPadIndices() map[int]bool {
	out := make(map[int]bool, len(PreferredPackagePads))
	for _, name := range PreferredPackagePads {
		if i, ok := padIndexByName(name); ok {
			out[i] = true
		}
	}
	return out
}

func padIndexByName(name string) (int, bool) {
	for i, p := range IsolationPads {
		if p.Name == name {
			return i, true
		}
	}
	return 0, false
}

// suiteKeyFromCaller returns e.g. "combat/threat" or "smoke" from the test file path.
func suiteKeyFromCaller() string {
	for skip := 2; skip < 16; skip++ {
		_, file, _, ok := runtime.Caller(skip)
		if !ok {
			break
		}
		file = filepath.ToSlash(file)
		if i := strings.Index(file, "/e2e/suites/"); i >= 0 {
			rest := file[i+len("/e2e/suites/"):]
			dir := filepath.ToSlash(filepath.Dir(rest))
			if dir == "." || dir == "" {
				continue
			}
			return dir
		}
		if strings.Contains(file, "/e2e/smoke/") {
			return "smoke"
		}
	}
	return "default"
}

// DefaultNearPadDist is the default max distance for AssertNearPad after .go xyz.
const DefaultNearPadDist float32 = 15

// TeleNamed runs `.tele <name>` and waits for far transfer completion.
// Packet-driven: WaitForTeleportAfter (SMSG_NEW_WORLD / MOVE_TELEPORT) then PhaseInWorld.
// Callers that need nearby units must still WaitUnit / WaitNearbyUnitByEntry after return.
func TeleNamed(t *testing.T, w *client.WorldClient, name string) {
	t.Helper()
	beforeMap := uint32(0)
	_, _, _, beforeMap = Position(w)
	before := w.TeleportSeq()
	MustGM(t, w, fmt.Sprintf(".tele %s", name))
	// Far transfers use SMSG_NEW_WORLD; near pads use SMSG_MOVE_TELEPORT — both bump TeleportSeq.
	// WaitForLogin is a one-shot login channel and returns immediately after first world entry.
	if err := w.WaitForTeleportAfter(before, 60*time.Second); err != nil {
		t.Logf("WaitForTeleportAfter after .tele %s: %v (continuing)", name, err)
	}
	WaitInWorld(t, w, 15*time.Second)
	x, y, z, afterMap := Position(w)
	t.Logf("TeleNamed %q -> %.1f,%.1f,%.1f map=%d (was map=%d)", name, x, y, z, afterMap, beforeMap)
}

// GoCreatureGUID teleports to a creature by DB spawn guid (`.go creature N`).
func GoCreatureGUID(t *testing.T, w *client.WorldClient, spawnGUID uint32) {
	t.Helper()
	MustGMTeleport(t, w, fmt.Sprintf(".go creature %d", spawnGUID))
}

// GoCreatureID teleports to a creature by template entry (`.go creature id N`).
// Prefer this after `.tele` when the pad is short of melee range.
func GoCreatureID(t *testing.T, w *client.WorldClient, entry uint32) {
	t.Helper()
	MustGMTeleport(t, w, fmt.Sprintf(".go creature id %d", entry))
}

// TeleportPad teleports to a named pad via .go xyz.
func TeleportPad(t *testing.T, w *client.WorldClient, pad Position3) {
	t.Helper()
	TeleportGo(t, w, pad.X, pad.Y, pad.Z, pad.Map)
}

// AssertNear fatals as precondition when the bot is farther than maxDist from (x,y,z).
// maxDist <= 0 defaults to DefaultNearPadDist.
func (b *ScenarioBot) AssertNear(t *testing.T, x, y, z, maxDist float32) {
	t.Helper()
	if maxDist <= 0 {
		maxDist = DefaultNearPadDist
	}
	px, py, pz, _ := b.Pos()
	d := Distance3D(px, py, pz, x, y, z)
	if d > maxDist {
		Preconditionf(t, "%s not near target pos: dist=%.2f max=%.2f at=(%.1f,%.1f,%.1f) want=(%.1f,%.1f,%.1f)",
			b.Name, d, maxDist, px, py, pz, x, y, z)
	}
}

// AssertNearPad fatals when the bot is not near pad (and optionally wrong map).
// maxDist <= 0 defaults to DefaultNearPadDist. Map must match pad.Map.
func (b *ScenarioBot) AssertNearPad(t *testing.T, pad Position3, maxDist float32) {
	t.Helper()
	if maxDist <= 0 {
		maxDist = DefaultNearPadDist
	}
	px, py, pz, mapID := b.Pos()
	if mapID != pad.Map {
		Preconditionf(t, "%s wrong map after tele: got=%d want=%d", b.Name, mapID, pad.Map)
	}
	d := Distance3D(px, py, pz, pad.X, pad.Y, pad.Z)
	if d > maxDist {
		Preconditionf(t, "%s not near pad: dist=%.2f max=%.2f at=(%.1f,%.1f,%.1f) pad=(%.1f,%.1f,%.1f map=%d)",
			b.Name, d, maxDist, px, py, pz, pad.X, pad.Y, pad.Z, pad.Map)
	}
}

// AssertMoved fatals when the bot has not moved at least minDist from (fromX,fromY,fromZ).
// Returns the measured distance. minDist <= 0 defaults to 1 yard.
// Use for charge / knockback / escort motion smoke checks.
func (b *ScenarioBot) AssertMoved(t *testing.T, fromX, fromY, fromZ, minDist float32) float32 {
	t.Helper()
	if minDist <= 0 {
		minDist = 1
	}
	px, py, pz, _ := b.Pos()
	d := Distance3D(fromX, fromY, fromZ, px, py, pz)
	if d < minDist {
		Preconditionf(t, "%s did not move enough: dist=%.2f min=%.2f from=(%.1f,%.1f,%.1f) now=(%.1f,%.1f,%.1f)",
			b.Name, d, minDist, fromX, fromY, fromZ, px, py, pz)
	}
	return d
}

// DistFrom returns 3D distance from the bot's current position to (x,y,z).
func (b *ScenarioBot) DistFrom(x, y, z float32) float32 {
	px, py, pz, _ := b.Pos()
	return Distance3D(px, py, pz, x, y, z)
}
