-- scripts/ai/data/deathknight_spells.lua
-- DK (blood/unholy/frost) 3.3.5a researched from setup + rotations.

local M = {}

M.SPELLS = {
  -- basic
  ICY_TOUCH = 45477,
  PLAGUE_STRIKE = 45462,
  BLOOD_STRIKE = 45902,
  DEATH_COIL = 47541,
  DEATH_STRIKE = 49998,
  -- runes
  RUNE_STRIKE = 56815,
  -- blood
  HEART_STRIKE = 55050,
  BLOOD_BOIL = 48721,
  MARK_OF_BLOOD = 49005,
  -- frost
  OBLITERATE = 49020,
  HOWLING_BLAST = 49184,
  FROST_STRIKE = 49143,
  -- unholy
  SCOURGE_STRIKE = 55090,
  DEATH_AND_DECAY = 49936,
  -- utility
  DEATH_GRIP = 49576,
  CHAINS_OF_ICE = 45524,
  HORN_OF_WINTER = 57330,
  -- presences
  BLOOD_PRESENCE = 48266,
  FROST_PRESENCE = 48263,
  UNHOLY_PRESENCE = 48265,
}

M.AURAS = {
  HORN_OF_WINTER = 57330,
  BLOOD_PRESENCE = 48266,
}

return M
