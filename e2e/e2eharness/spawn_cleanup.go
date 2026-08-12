package e2eharness

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// Game object / creature entries that e2e has been known to litter.
const (
	// GameObjectGiftOfTheObserver is Algalon's chest (GO 194821) used by #26894 loot tests.
	// `.gobject add` is a persistent DB spawn — always DespawnGameObjectSpawn after use.
	GameObjectGiftOfTheObserver uint32 = 194821

	// spawnDBSettle is how long to wait for worldserver to flush a new `.npc add` / `.gobject add`
	// row before reading creature/gameobject.guid.
	spawnDBSettle = 400 * time.Millisecond
	// spawnDeleteSettle is how long to leave the socket open after a live despawn command
	// so the chat handler actually runs (commands are async).
	spawnDeleteSettle = 350 * time.Millisecond
)

// LatestCreatureSpawnNear returns the highest creature.guid for entry near (x,y) on map.
func LatestCreatureSpawnNear(db *sql.DB, entry uint32, mapID uint32, x, y, radius float32) (spawnID uint32, ok bool, err error) {
	if db == nil {
		return 0, false, fmt.Errorf("world db nil")
	}
	if radius <= 0 {
		radius = 25
	}
	err = db.QueryRow(`
		SELECT guid FROM creature
		WHERE id=? AND map=?
		  AND position_x BETWEEN ? AND ?
		  AND position_y BETWEEN ? AND ?
		ORDER BY guid DESC LIMIT 1`,
		entry, mapID,
		x-radius, x+radius,
		y-radius, y+radius,
	).Scan(&spawnID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return spawnID, true, nil
}

// LatestGameObjectSpawnNear returns the highest gameobject.guid for entry near (x,y) on map.
func LatestGameObjectSpawnNear(db *sql.DB, entry uint32, mapID uint32, x, y, radius float32) (spawnID uint32, ok bool, err error) {
	if db == nil {
		return 0, false, fmt.Errorf("world db nil")
	}
	if radius <= 0 {
		radius = 25
	}
	err = db.QueryRow(`
		SELECT guid FROM gameobject
		WHERE id=? AND map=?
		  AND position_x BETWEEN ? AND ?
		  AND position_y BETWEEN ? AND ?
		ORDER BY guid DESC LIMIT 1`,
		entry, mapID,
		x-radius, x+radius,
		y-radius, y+radius,
	).Scan(&spawnID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return spawnID, true, nil
}

// ListCreatureSpawnsNear returns all creature.guid for entry near (x,y) on map.
func ListCreatureSpawnsNear(db *sql.DB, entry uint32, mapID uint32, x, y, radius float32) ([]uint32, error) {
	if db == nil {
		return nil, fmt.Errorf("world db nil")
	}
	if radius <= 0 {
		radius = 100
	}
	rows, err := db.Query(`
		SELECT guid FROM creature
		WHERE id=? AND map=?
		  AND position_x BETWEEN ? AND ?
		  AND position_y BETWEEN ? AND ?
		ORDER BY guid`,
		entry, mapID,
		x-radius, x+radius,
		y-radius, y+radius,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uint32
	for rows.Next() {
		var g uint32
		if err := rows.Scan(&g); err != nil {
			return out, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListGameObjectSpawnsNear returns all gameobject.guid for entry near (x,y) on map.
func ListGameObjectSpawnsNear(db *sql.DB, entry uint32, mapID uint32, x, y, radius float32) ([]uint32, error) {
	if db == nil {
		return nil, fmt.Errorf("world db nil")
	}
	if radius <= 0 {
		radius = 100
	}
	rows, err := db.Query(`
		SELECT guid FROM gameobject
		WHERE id=? AND map=?
		  AND position_x BETWEEN ? AND ?
		  AND position_y BETWEEN ? AND ?
		ORDER BY guid`,
		entry, mapID,
		x-radius, x+radius,
		y-radius, y+radius,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uint32
	for rows.Next() {
		var g uint32
		if err := rows.Scan(&g); err != nil {
			return out, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// withWorldDB opens world DB for one call when b has none cached.
// Uses WorldDSN (E2E_WORLD_DSN). Failures are logged — callers must not assume spawn-id cleanup ran.
func (b *ScenarioBot) withWorldDB(t *testing.T) *sql.DB {
	t.Helper()
	if b != nil && b.WorldDB != nil {
		return b.WorldDB
	}
	db, err := OpenWorldDB()
	if err != nil {
		t.Logf("OpenWorldDB (%s): %v — spawn SQL cleanup DISABLED", WorldDSN, err)
		return nil
	}
	t.Cleanup(func() { _ = db.Close() })
	if b != nil {
		b.WorldDB = db
	}
	return db
}

// sqlDeleteCreatureSpawn removes the row from acore_world.creature (sync, survives logout).
func sqlDeleteCreatureSpawn(db *sql.DB, spawnID uint32) error {
	if db == nil || spawnID == 0 {
		return nil
	}
	_, err := db.Exec(`DELETE FROM creature WHERE guid=?`, spawnID)
	return err
}

// sqlDeleteGameObjectSpawn removes the row from acore_world.gameobject.
func sqlDeleteGameObjectSpawn(db *sql.DB, spawnID uint32) error {
	if db == nil || spawnID == 0 {
		return nil
	}
	_, err := db.Exec(`DELETE FROM gameobject WHERE guid=?`, spawnID)
	return err
}

// canSendGM is true only when the world socket is still open. SessionAlive alone
// is wrong after Close: phase can stay non-zero while writes fail with
// "use of closed network connection".
func (b *ScenarioBot) canSendGM() bool {
	return b != nil && b.World != nil && !b.World.IsStopped()
}

// softGM sends a GM chat command without failing the test. Used only from cleanup.
func softGM(b *ScenarioBot, cmd string) {
	if b == nil || !b.canSendGM() {
		return
	}
	_ = b.World.SendGMCommand(cmd) // ignore closed-conn errors
}

// DespawnCreatureSpawn removes a persistent creature spawn completely:
//  1. SQL DELETE always (sync; survives Session.Close) — this is the real cleanup
//  2. Optional live `.npc delete N` only if the socket is still open (never MustGM)
//
// Cleanup must not call MustGM: t.Cleanup order can run after Close, and MustGM
// fatals with "use of closed network connection".
func (b *ScenarioBot) DespawnCreatureSpawn(t *testing.T, spawnID uint32) {
	t.Helper()
	if spawnID == 0 {
		return
	}
	db := b.withWorldDB(t)
	if err := sqlDeleteCreatureSpawn(db, spawnID); err != nil {
		t.Logf("SQL DELETE creature guid=%d: %v", spawnID, err)
	} else if db != nil {
		t.Logf("SQL deleted creature spawn %d", spawnID)
	}
	if !b.canSendGM() {
		return
	}
	softGM(b, ".gm on")
	softGM(b, fmt.Sprintf(".npc delete %d", spawnID))
	time.Sleep(spawnDeleteSettle)
}

// DespawnGameObjectSpawn removes a persistent GO: SQL DELETE + optional live delete.
func (b *ScenarioBot) DespawnGameObjectSpawn(t *testing.T, spawnID uint32) {
	t.Helper()
	if spawnID == 0 {
		return
	}
	db := b.withWorldDB(t)
	if err := sqlDeleteGameObjectSpawn(db, spawnID); err != nil {
		t.Logf("SQL DELETE gameobject guid=%d: %v", spawnID, err)
	} else if db != nil {
		t.Logf("SQL deleted gameobject spawn %d", spawnID)
	}
	if !b.canSendGM() {
		return
	}
	softGM(b, ".gm on")
	softGM(b, fmt.Sprintf(".gobject delete %d", spawnID))
	time.Sleep(spawnDeleteSettle)
}

// DespawnNPC select+`.npc delete` for a unit still in the client cache (when no DB id).
// Soft only — never fails the test if the socket is already closed.
func (b *ScenarioBot) DespawnNPC(t *testing.T, guid uint64) {
	t.Helper()
	if guid == 0 || !b.canSendGM() {
		return
	}
	if b.World.GetObject(guid) == nil {
		return
	}
	softGM(b, ".gm on")
	_ = b.World.SetTarget(guid)
	time.Sleep(80 * time.Millisecond)
	if !b.canSendGM() {
		return
	}
	softGM(b, ".npc delete")
	time.Sleep(spawnDeleteSettle)
}

// DespawnNearbyEntry deletes cache units of entry AND every matching DB spawn near the bot.
func (b *ScenarioBot) DespawnNearbyEntry(t *testing.T, entry uint32, maxDist float32) {
	t.Helper()
	if maxDist <= 0 {
		maxDist = 100
	}
	if b.canSendGM() {
		softGM(b, ".gm on")
		for _, u := range b.UnitsByEntry(maxDist, entry) {
			b.DespawnNPC(t, u.GUID)
		}
	}
	px, py, _, mapID := float32(0), float32(0), float32(0), uint32(0)
	if b != nil && b.World != nil {
		px, py, _, mapID = b.Pos()
	}
	if db := b.withWorldDB(t); db != nil {
		ids, err := ListCreatureSpawnsNear(db, entry, mapID, px, py, maxDist)
		if err != nil {
			t.Logf("ListCreatureSpawnsNear entry=%d: %v", entry, err)
			return
		}
		for _, id := range ids {
			b.DespawnCreatureSpawn(t, id)
		}
	}
}

// DespawnNearbyGameObjectEntry deletes all DB gameobjects of entry near the bot.
func (b *ScenarioBot) DespawnNearbyGameObjectEntry(t *testing.T, entry uint32, maxDist float32) {
	t.Helper()
	if maxDist <= 0 {
		maxDist = 100
	}
	px, py, _, mapID := b.Pos()
	db := b.withWorldDB(t)
	if db == nil {
		return
	}
	ids, err := ListGameObjectSpawnsNear(db, entry, mapID, px, py, maxDist)
	if err != nil {
		t.Logf("ListGameObjectSpawnsNear entry=%d: %v", entry, err)
		return
	}
	for _, id := range ids {
		b.DespawnGameObjectSpawn(t, id)
	}
}

// SpawnGameObject runs `.gobject add <entry>`, resolves DB spawn id (with settle+retry),
// registers t.Cleanup that SQL-deletes + live-deletes, returns spawn id (0 if unresolved).
func (b *ScenarioBot) SpawnGameObject(t *testing.T, entry uint32) uint32 {
	t.Helper()
	b.DespawnNearbyGameObjectEntry(t, entry, 80)

	px, py, _, mapID := b.Pos()
	MustGM(t, b.World, ".gm on")
	MustGM(t, b.World, fmt.Sprintf(".gobject add %d", entry))

	spawnID := b.waitCaptureGOSpawn(t, entry, mapID, px, py)
	if spawnID != 0 {
		sid := spawnID
		t.Cleanup(func() { b.DespawnGameObjectSpawn(t, sid) })
		t.Logf("SpawnGameObject entry=%d spawnId=%d", entry, spawnID)
	} else {
		t.Logf("SpawnGameObject entry=%d: FAILED to resolve spawn id (WorldDSN=%s) — WILL LITTER", entry, WorldDSN)
	}
	return spawnID
}

func (b *ScenarioBot) waitCaptureGOSpawn(t *testing.T, entry, mapID uint32, x, y float32) uint32 {
	t.Helper()
	db := b.withWorldDB(t)
	if db == nil {
		return 0
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		id, ok, err := LatestGameObjectSpawnNear(db, entry, mapID, x, y, 30)
		if err != nil {
			t.Logf("LatestGameObjectSpawnNear: %v", err)
			return 0
		}
		if ok {
			return id
		}
		time.Sleep(spawnDBSettle)
	}
	return 0
}

// CaptureCreatureSpawnID records the latest DB spawn for entry near the bot (after `.npc add`).
// Retries until settle — immediate SELECT after chat often races an empty result.
func (b *ScenarioBot) CaptureCreatureSpawnID(t *testing.T, entry uint32) uint32 {
	t.Helper()
	px, py, _, mapID := b.Pos()
	db := b.withWorldDB(t)
	if db == nil {
		return 0
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		id, ok, err := LatestCreatureSpawnNear(db, entry, mapID, px, py, 30)
		if err != nil {
			t.Logf("CaptureCreatureSpawnID: %v", err)
			return 0
		}
		if ok {
			return id
		}
		time.Sleep(spawnDBSettle)
	}
	t.Logf("CaptureCreatureSpawnID entry=%d: no row near (%.0f,%.0f) WorldDSN=%s", entry, px, py, WorldDSN)
	return 0
}

// SpawnPersistent adds a DB creature (`.npc add`, not temp), waits for live GUID,
// captures spawn id, registers SQL+live cleanup. Use for training dummies / pull targets.
//
// Do NOT use `.npc add temp` for e2e fixtures: temps are not in `creature` table,
// live ~120s (TEMPSUMMON_CORPSE_DESPAWN), and select-delete races Session.Close.
func (b *ScenarioBot) SpawnPersistent(t *testing.T, entry uint32, timeout time.Duration) (liveGUID uint64, spawnID uint32) {
	t.Helper()
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	b.DespawnNearbyEntry(t, entry, 100)

	known := map[uint64]struct{}{}
	for _, u := range b.UnitsByEntry(100, entry) {
		known[u.GUID] = struct{}{}
	}

	MustGM(t, b.World, ".gm on")
	MustGM(t, b.World, fmt.Sprintf(".npc add %d", entry))
	spawnID = b.CaptureCreatureSpawnID(t, entry)

	newOnes := b.WaitNewUnits(t, known, []uint32{entry}, timeout)
	if len(newOnes) == 0 {
		Preconditionf(t, "SpawnPersistent: entry %d not in object cache after .npc add", entry)
	}
	// Closest to bot.
	liveGUID = newOnes[0].GUID
	best := float32(1e9)
	px, py, pz, _ := b.Pos()
	for _, u := range newOnes {
		obj := b.World.GetObject(u.GUID)
		if obj == nil || !obj.HasKnownPosition() {
			continue
		}
		ox, oy, oz := obj.InterpolatedPosition()
		d := Distance3D(px, py, pz, ox, oy, oz)
		if d < best {
			best = d
			liveGUID = u.GUID
		}
	}
	t.Logf("SpawnPersistent entry=%d live=0x%X dbSpawn=%d dist=%.1f", entry, liveGUID, spawnID, best)

	sid, lg := spawnID, liveGUID
	t.Cleanup(func() {
		if sid != 0 {
			b.DespawnCreatureSpawn(t, sid)
			return
		}
		// Last resort: select delete (may no-op if cache empty / session dead).
		b.DespawnNPC(t, lg)
		t.Logf("SpawnPersistent cleanup: no dbSpawn for entry live=0x%X — may litter until purge", lg)
	})
	return liveGUID, spawnID
}
