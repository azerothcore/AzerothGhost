-- scripts/ai/data/siege_gear.lua
-- Curated high-end lvl 80 gear tables extracted/derived from reference/*.html armory pages.
-- Each entry maps classID (1=warrior ... 11=druid) -> spec -> slot -> itemID
-- Used by siege_prep to .additem during GM prep phase.
-- Slots are illustrative; prep sends .additem for each value. Real servers may ignore dupes or wrong slots.
-- Fallback to decent items if server DB lacks exact IDs.
-- Update process: re-grep reference/*.html for "item=NNNNN" from fresh armory pages or manual review.

local M = {}

-- Class IDs match WoW: 1 warrior, 2 paladin, 3 hunter, 4 rogue, 5 priest, 6 dk, 7 shaman, 8 mage, 9 warlock, 11 druid
local GEAR = {
  -- Warrior (fury/arms focus from warrior.html)
  [1] = {
    fury = {
      head=51212, shoulders=47429, chest=50982, wrists=49906, hands=49899,
      waist=50362, legs=49906, feet=49888, mainhand=47285, offhand=48711,
      trinket1=47131, trinket2=50362, ring1=45608, ring2=47414, neck=47429,
    },
    arms = {
      head=51212, shoulders=47429, chest=50982, mainhand=49888, trinket1=47131,
    },
    prot = { head=51212, shoulders=47429, chest=50982, mainhand=47285 },
    default = { head=51212, shoulders=47429, chest=50982, mainhand=47285 },
  },

  -- Hunter (from hunter.html)
  [3] = {
    beast_mastery = {
      head=47472, shoulders=48046, chest=47480, mainhand=46969, ranged=47428,
      trinket1=47115, trinket2=47131,
    },
    default = { head=47472, shoulders=48046, chest=47480, mainhand=46969, ranged=47428 },
  },

  -- Mage (from mage.html)
  [8] = {
    fire = {
      head=47603, shoulders=47761, chest=47753, mainhand=47659, offhand=47661,
      trinket1=45518, trinket2=47143,
    },
    default = { head=47603, shoulders=47761, chest=47753, mainhand=47659 },
  },

  -- Paladin (prot from paladin-prot.html)
  [2] = {
    protection = {
      head=47626, shoulders=47624, chest=47640, mainhand=47661, shield=44312,
      trinket1=47115, trinket2=47131,
    },
    retribution = { head=47626, shoulders=47624, chest=47640, mainhand=47661 },
    holy = { head=47626, shoulders=47624, chest=47640, mainhand=47661 },
    default = { head=47626, shoulders=47624, chest=47640, mainhand=47661 },
  },

  -- Priest (from priest.html)
  [5] = {
    shadow = {
      head=40127, shoulders=47429, chest=40182, mainhand=47059, offhand=40178,
      trinket1=42158, trinket2=47429,
    },
    holy_priest = { head=40127, shoulders=47429, chest=40182, mainhand=47059 },
    discipline = { head=40127, shoulders=47429, chest=40182, mainhand=47059 },
    default = { head=40127, shoulders=47429, chest=40182, mainhand=47059 },
  },

  -- Rogue (from rogue.html)
  [4] = {
    assassination = {
      head=47464, shoulders=47429, chest=48055, mainhand=47545, offhand=49110,
      trinket1=47115, trinket2=47429,
    },
    combat = { head=47464, shoulders=47429, chest=48055, mainhand=47545, offhand=49110 },
    default = { head=47464, shoulders=47429, chest=48055, mainhand=47545 },
  },

  -- DK (use warrior-like plate + common)
  [6] = {
    blood = { head=51212, shoulders=47429, chest=50982, mainhand=47285, trinket1=47131 },
    frost_dk = { head=51212, shoulders=47429, chest=50982, mainhand=49888 },
    unholy = { head=51212, shoulders=47429, chest=50982, mainhand=47285 },
    default = { head=51212, shoulders=47429, chest=50982, mainhand=47285 },
  },

  -- Shaman (partial from common + druid/hunter cross)
  [7] = {
    elemental = { head=47472, shoulders=48046, chest=47480, mainhand=46969 },
    enhancement = { head=47472, shoulders=48046, chest=47480, mainhand=46969 },
    resto_shaman = { head=47472, shoulders=48046, chest=47480, mainhand=46969 },
    default = { head=47472, shoulders=48046, chest=47480, mainhand=46969 },
  },

  -- Warlock (use mage-like cloth + common)
  [9] = {
    destruction = { head=47603, shoulders=47761, chest=47753, mainhand=47659 },
    affliction = { head=47603, shoulders=47761, chest=47753, mainhand=47659 },
    demonology = { head=47603, shoulders=47761, chest=47753, mainhand=47659 },
    default = { head=47603, shoulders=47761, chest=47753, mainhand=47659 },
  },

  -- Druid (from druid-restoration.html + common)
  [11] = {
    resto_druid = {
      head=47096, shoulders=47190, chest=47096, mainhand=46017,
      trinket1=45929, trinket2=47041,
    },
    feral = { head=47096, shoulders=47190, chest=47096, mainhand=46017 },
    balance = { head=47096, shoulders=47190, chest=47096, mainhand=46017 },
    default = { head=47096, shoulders=47190, chest=47096, mainhand=46017 },
  },
}

-- Consumables / utility (common to all)
M.CONSUMABLES = {
  health_pot = 33447, mana_pot = 33448, food=40093, bandage=34721,
  arrows = 52021, shards=6265, -- hunter/lock specific
}

function M.get_gear(class, spec)
  spec = spec or "default"
  local cls_gear = GEAR[class] or {}
  local set = cls_gear[spec] or cls_gear.default or {}
  -- flatten to list of item ids
  local list = {}
  for _, id in pairs(set) do
    table.insert(list, id)
  end
  return list
end

function M.get_consumables(class)
  local base = {M.CONSUMABLES.health_pot, M.CONSUMABLES.mana_pot, M.CONSUMABLES.food}
  if class == 3 then table.insert(base, M.CONSUMABLES.arrows) end
  if class == 9 then table.insert(base, M.CONSUMABLES.shards) end
  return base
end

M.GEAR = GEAR -- raw access if needed
return M
