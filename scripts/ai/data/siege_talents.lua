-- scripts/ai/data/siege_talents.lua
-- Key talent spell IDs (the ones granted by talent points) for major specs.
-- Use with .learn <id> during prep. These are the trained/spell form of talents.
-- Sources: class data/*_spells.lua cross-ref + known WotLK talent trees + armory pages.
-- Not exhaustive; focus on signature high-impact talents for realism in siege.

local M = {}

-- Warrior
M.warrior = {
  fury = { 23881, 12328, 12292, 29801, 29838 }, -- Bloodthirst, Sweeping Strikes, Death Wish, etc.
  arms = { 12294, 29623, 29834, 12809 },        -- Mortal Strike line, etc.
  prot = { 23922, 12809, 12975, 20243 },
}

-- Paladin
M.paladin = {
  protection = { 31935, 20911, 20912, 20913, 20914, 27168, 48942, 48943, 48945, 48947 },
  retribution = { 35395, 20066, 20216, 31866, 31867, 31868 },
  holy = { 20473, 20474, 20475, 25890, 31842 },
}

-- Hunter
M.hunter = {
  beast_mastery = { 19577, 19574, 19575, 19621, 19622, 19623, 34453, 34454, 34455 },
  default = { 19577, 19574 },
}

-- Rogue
M.rogue = {
  assassination = { 1329, 32645, 14177, 14186, 14190 },
  combat = { 53, 16511, 13750, 13877 },
  subtlety = { 36554, 14183, 14185 },
}

-- Priest
M.priest = {
  shadow = { 15473, 15286, 15285, 15284, 15487, 15473 },
  holy_priest = { 2060, 2050, 34863, 34864, 34865, 34866, 47540, 47788 },
  discipline = { 17, 592, 600, 47540, 33206, 10060 },
}

-- Death Knight
M.dk = {
  blood = { 55050, 48982, 49005, 49016, 49028 },
  frost_dk = { 49184, 49143, 49184, 51271, 49143 },
  unholy = { 55090, 49194, 49206, 49181 },
}

-- Shaman
M.shaman = {
  elemental = { 51505, 51490, 51485, 51486, 16166 },
  enhancement = { 17364, 51533, 30823, 30823 },
  resto_shaman = { 1064, 16190, 16188, 16187, 16176 },
}

-- Mage
M.mage = {
  fire = { 11113, 31661, 11113, 42944, 42945, 42946, 42947, 42948 },
  default = { 11113, 31661 },
}

-- Warlock
M.warlock = {
  destruction = { 17962, 30283, 30284, 30285, 30288 },
  affliction = { 30108, 30109, 30110, 30111, 30112, 48181 },
  demonology = { 47241, 50589, 50590 },
}

-- Druid
M.druid = {
  resto_druid = { 18562, 17116, 33891, 48438, 48441, 48443, 48444, 48445, 48446, 48447 },
  feral = { 33876, 33878, 33943, 50334, 50334 },
  balance = { 24858, 48389, 48392, 48393, 48394, 48395, 48432, 48433, 48434, 48435 },
}

function M.get_talents(class, spec)
  spec = spec or "default"
  if class == 1 then return M.warrior[spec] or M.warrior.fury or {} end
  if class == 2 then return M.paladin[spec] or M.paladin.protection or {} end
  if class == 3 then return M.hunter[spec] or M.hunter.default or {} end
  if class == 4 then return M.rogue[spec] or M.rogue.assassination or {} end
  if class == 5 then return M.priest[spec] or M.priest.shadow or {} end
  if class == 6 then return M.dk[spec] or M.dk.blood or {} end
  if class == 7 then return M.shaman[spec] or M.shaman.elemental or {} end
  if class == 8 then return M.mage[spec] or M.mage.default or {} end
  if class == 9 then return M.warlock[spec] or M.warlock.destruction or {} end
  if class == 11 then return M.druid[spec] or M.druid.resto_druid or {} end
  return {}
end

return M
