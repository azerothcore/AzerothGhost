-- scripts/lib/combat.lua — cast gating + melee openers
local util = dofile("scripts/lib/util.lua")

local M = {}

function M.new_caster(opts)
  opts = opts or {}
  local self = {
    gcd = opts.gcd or 1.5,
    costs = opts.costs or {},
    last_cast_at = 0,
    sheathed = false,
  }

  function self:ready_melee(guid, do_face)
    if not self.sheathed and bot.set_sheath then
      pcall(function()
        bot.set_sheath(0)
      end)
      self.sheathed = true
    end
    if do_face and bot.face_target and not util.is_zero_guid(guid) then
      pcall(function()
        bot.face_target(guid)
      end)
    end
  end

  function self:reset_pull()
    self.sheathed = false
  end

  function self:try_cast(spell_id, target_guid, extra)
    extra = extra or {}
    if not spell_id or spell_id == 0 then
      return false
    end
    if bot.is_spell_ready and not bot.is_spell_ready(spell_id) then
      return false
    end
    local need = self.costs[spell_id] or extra.rage or 0
    if util.rage() < need then
      return false
    end
    local t = util.now()
    if not extra.ignore_gcd and (t - self.last_cast_at) < self.gcd then
      return false
    end
    if target_guid and not util.is_zero_guid(target_guid) then
      self:ready_melee(target_guid, true)
    end
    if bot.cast_spell then
      bot.cast_spell(spell_id, target_guid or 0)
    end
    self.last_cast_at = t
    return true
  end

  function self:attack(guid)
    if bot.attack and not util.is_zero_guid(guid) then
      bot.attack(guid)
      return true
    end
    return false
  end

  return self
end

return M
