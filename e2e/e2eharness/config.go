// Package e2eharness provides a reusable library for live-stack 3.3.5a e2e tests
// against AzerothCore (standalone or behind a client gateway). Downstream modules
// import this package and write their own tests; see e2e/EXAMPLES.md.
package e2eharness

import "os"

// Connection settings for the target AzerothCore environment.
// Override via env without code changes (point at your AC or gateway entrypoint):
//
//	E2E_AUTH_ADDR  (default 127.0.0.1:3724)
//	E2E_AUTH_DSN   (default acore:acore@tcp(127.0.0.1:3306)/acore_auth)
//	E2E_CHAR_DSN   (default acore:acore@tcp(127.0.0.1:3306)/acore_characters)
//	E2E_WORLD_DSN  (default acore:acore@tcp(127.0.0.1:3306)/acore_world)
var (
	AuthAddr = envOr("E2E_AUTH_ADDR", "127.0.0.1:3724")
	AuthDSN  = envOr("E2E_AUTH_DSN", "acore:acore@tcp(127.0.0.1:3306)/acore_auth")
	CharDSN  = envOr("E2E_CHAR_DSN", "acore:acore@tcp(127.0.0.1:3306)/acore_characters")
	WorldDSN = envOr("E2E_WORLD_DSN", "acore:acore@tcp(127.0.0.1:3306)/acore_world")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Gameplay / protocol constants for guild e2e.
const (
	// Default password used by ensure-account helpers.
	DefaultPassword = "test"

	// MinPetitionSigns matches charserver default MIN_PETITION_SIGNS.
	MinPetitionSigns = 9

	// Guild charter item entry (Guild Charter).
	ItemGuildCharter = 5863

	// Linen Cloth — stackable, tradable, cheap test deposit.
	ItemLinenCloth = 2589

	// Stormwind guild vault (gameobject type 34) — from acore_world:
	//   SELECT guid,id,map,position_x,position_y,position_z,orientation
	//   FROM gameobject g JOIN gameobject_template gt ON gt.entry=g.id
	//   WHERE gt.type=34 AND g.map=0 AND g.guid=41911;
	StormwindGuildVaultGUIDLow = uint32(41911)
	StormwindGuildVaultEntry   = uint32(187329)
	StormwindGuildVaultMap     = uint32(0)
	StormwindGuildVaultX       = float32(-8934.91)
	StormwindGuildVaultY       = float32(618.273)
	StormwindGuildVaultZ       = float32(100.589)
	StormwindGuildVaultO       = float32(0.506145)

	// Alternate SW vaults (same entry 187329) if the primary is out of range.
	// guid 41912: -8910.43, 636.38, 100.91
	// guid 41913: -8930.72, 610.526, 100.595
	// guid 41914: -8902.25, 621.314, 100.916

	// Stormwind tabard designer (guild charter) — from acore_world:
	//   SELECT c.guid,c.id,ct.name,c.map,c.position_x,c.position_y,c.position_z,c.orientation
	//   FROM creature c JOIN creature_template ct ON ct.entry=c.id
	//   WHERE c.guid=79681;  -- Aldwin Laughlin, npcflag has TABARDDESIGNER|PETITIONER
	StormwindTabardDesignerEntry   = uint32(4974)
	StormwindTabardDesignerGUIDLow = uint32(79681)
	StormwindTabardDesignerMap     = uint32(0)
	StormwindTabardDesignerX       = float32(-8885.25)
	StormwindTabardDesignerY       = float32(614.395)
	StormwindTabardDesignerZ       = float32(95.3576)
	StormwindTabardDesignerO       = float32(3.52556)

	// Rebecca Laughlin (nearby tabard, spawn 79685) if Aldwin is unavailable:
	// entry 5193, -8893.57, 611.211, 95.3409, o=0.314159

	// Default guild charter cost (10 gold) and bank first tab (100 gold).
	GuildCharterCostCopper = uint32(100000)
	GuildBankFirstTabCost  = uint32(1000000)
	// Backpack: bag=255, slots 23..38 (INVENTORY_SLOT_ITEM_START..END-1).
	InventoryBagBackpack   = uint8(255)
	InventorySlotItemStart = uint8(23)
	InventorySlotItemEnd   = uint8(39)
)
