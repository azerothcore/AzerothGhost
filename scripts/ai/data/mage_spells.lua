-- scripts/ai/data/mage_spells.lua
-- Researched mage spells (fire/frost focus), 3.3.5a from setup + standard.

local M = {}

M.SPELLS = {
  FIREBALL = 133,
  FROSTBOLT = 116,
  FROST_NOVA = 122,
  CONE_OF_COLD = 120,
  BLIZZARD = 10,
  FLAMESTRIKE = 2120,
  FIRE_BLAST = 2136,
  POLYMORPH = 118,
  FROST_ARMOR = 168,
  SCORCH = 2948,
  PYROBLAST = 11366,
  BLAST_WAVE = 11113,
  LIVING_BOMB = 44457,
  ARCANE_MISSILES = 5143,
  ARCANE_BLAST = 42897,
  EVOCATION = 12051,
  ICE_LANCE = 30455,
  -- procs auras (self)
  HOT_STREAK = 48108,
  FINGERS_OF_FROST = 44544,
}

M.AURAS = {
  FROST_NOVA = 122,
  HOT_STREAK = 48108,
  FROST_ARMOR = 168,
}

return M
