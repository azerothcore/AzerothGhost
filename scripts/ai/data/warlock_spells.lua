-- scripts/ai/data/warlock_spells.lua
-- Warlock (aff/destro/demo) from setup + researched.

local M = {}

M.SPELLS = {
  SHADOW_BOLT = 686,
  CORRUPTION = 172,
  CURSE_OF_AGONY = 1014,
  IMMOLATE = 348,
  CONFLAGRATE = 17962,
  INCINERATE = 29722,
  SOUL_FIRE = 6353,
  CHAOS_BOLT = 50796,
  -- demo
  METAMORPHOSIS = 47241,
  IMMOLATION_AURA = 50589,
  -- aff
  HAUNT = 48181,
  UNSTABLE_AFFLICTION = 30108,
  -- common
  LIFE_TAP = 1454,
  FEAR = 5782,
  HOWL_OF_TERROR = 5484,
  -- pets
  SUMMON_IMP = 688,
  SUMMON_VOIDWALKER = 697,
  SUMMON_SUCCUBUS = 712,
  SUMMON_FELHUNTER = 691,
  -- dots
  SEED_OF_CORRUPTION = 27243,
}

M.AURAS = {
  CORRUPTION = 172,
  IMMOLATE = 348,
}

return M
