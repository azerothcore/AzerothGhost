-- scripts/ai/data/rogue_spells.lua
-- Researched for rogue (assass/combat/sub), 3.3.5a ids from setup + standard.

local M = {}

M.SPELLS = {
  SINISTER_STRIKE = 1752,
  EVISCERATE = 2098,
  BACKSTAB = 53,
  GOUGE = 1776,
  KICK = 1766,
  SLICE_AND_DICE = 5171,
  RUPTURE = 1943,
  KIDNEY_SHOT = 408,
  HEMORRHAGE = 16511,
  MUTILATE = 1329,
  ENVENOM = 32645,
  -- utility
  SPRINT = 2983,
  STEALTH = 1784,
  VANISH = 1856,
  PICK_LOCK = 1804,
  -- poisons/combat
  DEADLY_POISON = 2823,
  WOUND_POISON = 13219,
  -- subtlety
  SHADOWSTEP = 36554,
  PREMEDITATION = 14183,
}

M.AURAS = {
  SLICE_AND_DICE = 5171,
  STEALTH = 1784,
}

return M
