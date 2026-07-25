-- scripts/ai/data/druid_spells.lua
-- Druid (balance/feral/resto) researched.

local M = {}

M.SPELLS = {
  -- balance
  WRATH = 5176,
  MOONFIRE = 8921,
  STARFIRE = 2912,
  INSECT_SWARM = 5570,
  STARFALL = 48505,
  -- feral
  BEAR_FORM = 5487,
  CAT_FORM = 768,
  MOONKIN_FORM = 24858,
  MANGLE_BEAR = 33917,
  MANGLE_CAT = 33983,
  CLAW = 1082,
  RIP = 1079,
  RAKE = 1822,
  SHRED = 5221,
  MAUL = 6807,
  -- resto
  HEALING_TOUCH = 5185,
  REJUVENATION = 774,
  REGROWTH = 8936,
  SWIFTMEND = 18562,
  -- utility
  GROWL = 6795,
  BASH = 5211,
  FAERIE_FIRE = 770,
  MARK_OF_THE_WILD = 1126,
  THORNS = 467,
}

M.AURAS = {
  BEAR_FORM = 5487,
  CAT_FORM = 768,
  MOONKIN_FORM = 24858,
  MOONFIRE = 8921,
  REJUVENATION = 774,
}

return M
