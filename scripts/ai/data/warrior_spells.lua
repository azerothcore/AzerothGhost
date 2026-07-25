-- scripts/ai/data/warrior_spells.lua
-- Researched spell data for warrior (3.3.5a + playerbots + gamedata).
-- Used by class/warrior.lua
-- ASSUMPTION[WAR-001]: REND cast 772 applies aura 772 (verified via E2E on live AC)
-- LAST_VALIDATED: 2026-07-17 against azerothcore-clean (rend aura observed after cast in harness; PRACTICAL PLAN: high volume rend_casts + shout exercised under forced target lock + combat; aura has_aura fidelity partial due to timing - see validation-runs)
-- PRACTICAL_E2E: warrior rend/execute/shout P0 runs executed 2026-07-17 (casts observed, target forcing, no remote push)
-- BATTLE_SHOUT note: cast 2457 may apply different aura (6673 observed in runs); has_aura_on prefers aura id in checks where noted.

local M = {}

M.SPELLS = {
  -- Core from gamedata + grind.lua + behaviors + setup
  HEROIC_STRIKE = 78,
  REND = 772,
  THUNDER_CLAP = 6343,
  HAMSTRING = 1715,
  OVERPOWER = 7384,
  REVENGE = 6572,
  SUNDER_ARMOR = 7386,
  CLEAVE = 845,
  PUMMEL = 6552,
  SLAM = 1464,
  EXECUTE = 5308,
  WHIRLWIND = 1680,
  BATTLE_SHOUT = 2457,
  DEMORALIZING_SHOUT = 1160,
  INTIMIDATING_SHOUT = 5246,
  BERSERKER_RAGE = 18499,
  RECKLESSNESS = 1719,
  CHARGE = 100,
  TAUNT = 355,
  SHIELD_BLOCK = 2565,
  VICTORY_RUSH = 34428,
  -- Arms spec key (from playerbots ArmsWarrior)
  MORTAL_STRIKE = 12294,
  SWEEPING_STRIKES = 12328,
  PIERCING_HOWL = 12323,
  CONCUSSION_BLOW = 12809,
  -- Fury spec
  BLOODTHIRST = 23881,
  -- Prot spec
  SHIELD_SLAM = 23922,
  SHIELD_BASH = 72,
  SHIELD_WALL = 871,
  LAST_STAND = 12975,
  -- Stance spell ids (for reference; actual stance change often via 71/2457/2458 but not cast; STANCES table uses shape ids)
  -- BATTLE_STANCE = 2457, -- removed to avoid id conflict with BATTLE_SHOUT; not referenced by class impl
}

M.AURAS = {
  REND = 772,
  BATTLE_SHOUT = 6673, -- rank2 aura id approx, use spell for has
  SUNDER = 7386,
}

M.STANCES = {
  BATTLE = 1,
  DEFENSIVE = 2,
  BERSERKER = 3,
}

return M
