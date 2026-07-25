-- scripts/ai/data/hunter_spells.lua
-- Researched for hunter (BM focus pet), 3.3.5a ids from setup + behaviors + standard.
-- LAST_VALIDATED (PRACTICAL PLAN): 2026-07-17 - HUN-01/02 GREEN: get_pet_guid non-zero + pet_attack called in forced pet lifecycle run (class 3). See validation-runs/2026-07-17-e2e-runs.md + hunter_pet_cycle.lua


local M = {}

M.SPELLS = {
  RAPTOR_STRIKE = 2973,
  AUTO_SHOT = 75,
  CALL_PET = 883,
  REVIVE_PET = 982,
  DISMISS_PET = 2641,
  FEED_PET = 6991,
  CONCUSSIVE_SHOT = 5116,
  FREEZING_TRAP = 1499,
  VOLLEY = 1510,
  ARCANE_SHOT = 3044,
  SERPENT_STING = 1978,
  STEADY_SHOT = 56641,
  AIMED_SHOT = 19434,
  KILL_COMMAND = 34026,
  MULTI_SHOT = 2643,
  -- aspects
  ASPECT_HAWK = 13165,
  ASPECT_MONKEY = 13163,
  ASPECT_VIPER = 34074,
  ASPECT_DRAGONHAWK = 61846,
  -- pet mend
  MEND_PET = 136,
  -- pet spells
  GROWL = 2649,
}

M.AURAS = {
  SERPENT_STING = 1978,
  ASPECT_HAWK = 13165,
}

return M
