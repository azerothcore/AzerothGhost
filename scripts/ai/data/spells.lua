-- scripts/ai/data/spells.lua
-- Minimal shared spell data for generic AI. Expanded in class libs later.
-- Use for reference in triggers/actions; actual casts use numeric IDs.

local M = {}

M.GENERIC = {
  -- Common low level useful
  SHOOT = 3018,  -- generic wand/ranged shoot if applicable
}

M.POTIONS = {
  HEALING_POTION = 439,  -- example id; real use depends on having item
  MANA_POTION = 440,
}

M.AURAS = {
  -- common for detection, future use bot.has_aura etc.
}

-- Note: class-specific data in data/*_spells.lua loaded directly by class/ modules (own pcall for isolation).
-- Shared remains minimal for generics.

return M
