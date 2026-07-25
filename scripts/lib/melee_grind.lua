-- scripts/lib/melee_grind.lua
-- Generic sticky melee grind loop. Class-specific rotation via opts.rotation(ctx).
--
-- Usage:
--   local grind = dofile("scripts/lib/melee_grind.lua")
--   local g = grind.new({ rotation = function(ctx) ... end, spells = {...}, costs = {...} })
--   function on_tick() g:tick() end

local util = dofile("scripts/lib/util.lua")
local targeting = dofile("scripts/lib/targeting.lua")
local movement = dofile("scripts/lib/movement.lua")
local combat = dofile("scripts/lib/combat.lua")

local M = {}

function M.new(opts)
  opts = opts or {}
  local self = {
    scan_range = opts.scan_range or 40,
    melee_stop = opts.melee_stop or 2.8,
    melee_chase = opts.melee_chase or 4.5,
    charge_min = opts.charge_min or 8,
    charge_max = opts.charge_max or 25,
    settle = opts.settle or 0.5,
    charge_spell = opts.charge_spell,
    shout_spell = opts.shout_spell,
    shout_period = opts.shout_period or 45,
    rotation = opts.rotation, -- function(ctx) optional
    spells = opts.spells or {},
    boot_at = util.now(),
    current = "0",
    engaged = false,
    last_shout_at = 0,
    last_log_at = 0,
    chase = movement.new_chase(opts.chase or {}),
    wander = movement.new_wander(opts.wander or { period = 2.0, radius = 22 }),
    caster = combat.new_caster({ gcd = opts.gcd or 1.5, costs = opts.costs or {} }),
  }

  local function logf(fmt, ...)
    if bot.log then
      bot.log(string.format(fmt, ...))
    end
  end

  function self:clear()
    self.current = "0"
    self.engaged = false
    self.caster:reset_pull()
    self.chase:reset()
    targeting.clear_target()
  end

  function self:finish_corpse(guid, u)
    guid = util.guid_str(guid)
    -- Rats and many critters are not lootable — do not stall on LOOT.
    if u and u.lootable and bot.loot_all then
      bot.loot_all(guid)
    end
    targeting.blacklist(guid, opts.blacklist_ttl or 20)
    self:clear()
  end

  function self:maybe_shout()
    local sid = self.shout_spell
    if not sid then
      return
    end
    local t = util.now()
    if (t - self.last_shout_at) < self.shout_period then
      return
    end
    if util.rage() < (self.caster.costs[sid] or 10) then
      return
    end
    if bot.has_aura_on and bot.get_own_guid then
      if bot.has_aura_on(bot.get_own_guid(), sid) then
        self.last_shout_at = t
        return
      end
    end
    if self.caster:try_cast(sid, 0) then
      self.last_shout_at = t
    end
  end

  function self:default_rotation(ctx)
    -- Generic: execute / free proc / keep a DoT / dump rage / auto-attack
    local S = self.spells
    local r = util.rage()
    if S.EXECUTE and ctx.hp_pct < 20 and r >= 15 then
      if self.caster:try_cast(S.EXECUTE, ctx.guid) then
        return
      end
    end
    if S.VICTORY_RUSH and self.caster:try_cast(S.VICTORY_RUSH, ctx.guid) then
      return
    end
    if S.REND and bot.has_aura_on and not bot.has_aura_on(ctx.guid, S.REND) and r >= 10 then
      if self.caster:try_cast(S.REND, ctx.guid) then
        return
      end
    end
    if S.HEROIC_STRIKE and r >= 45 then
      if self.caster:try_cast(S.HEROIC_STRIKE, ctx.guid) then
        return
      end
    end
    if S.SUNDER_ARMOR and r >= 30 then
      self.caster:try_cast(S.SUNDER_ARMOR, ctx.guid)
    end
  end

  function self:combat(guid, u)
    guid = util.guid_str(guid)
    if not u or ((u.max_health or 0) > 0 and (u.health or 0) <= 0) or u.is_alive == false then
      self:finish_corpse(guid, u)
      return
    end

    local dist = u.distance or 999

    if dist > self.melee_chase or (dist > self.melee_stop and not self.engaged) then
      self.engaged = false
      self.chase:to_unit(u)
      return
    end

    movement.stop_if_moving()
    local entering = not self.engaged
    self.engaged = true
    self.caster:ready_melee(guid, entering)
    self.caster:attack(guid)

    local ctx = {
      guid = guid,
      unit = u,
      dist = dist,
      hp_pct = util.unit_hp_pct(u),
      rage = util.rage(),
      caster = self.caster,
      spells = self.spells,
    }
    if self.rotation then
      self.rotation(ctx)
    else
      self:default_rotation(ctx)
    end
  end

  function self:tick()
    if not bot.is_alive or not bot.is_alive() then
      if bot.send_command then
        bot.send_command(".revive")
      end
      self:clear()
      return
    end

    if (util.now() - self.boot_at) < self.settle then
      return
    end

    self:maybe_shout()

    local tgt = util.guid_str(bot.get_target and bot.get_target() or 0)
    if util.is_zero_guid(tgt) then
      tgt = self.current
    end

    local u = nil
    if not util.is_zero_guid(tgt) then
      u = bot.get_unit and bot.get_unit(tgt) or nil
    end

    if not util.is_zero_guid(tgt) then
      local dead = not u
        or u.is_alive == false
        or ((u.max_health or 0) > 0 and (u.health or 0) <= 0)
      if dead then
        self:finish_corpse(tgt, u)
        tgt, u = "0", nil
      end
    end

    if util.is_zero_guid(tgt) or not u then
      local best, sample, n = targeting.find_best_hostile({ max_dist = self.scan_range })
      if best then
        tgt = util.guid_str(best.guid)
        targeting.ensure_target(tgt)
        self.current = tgt
        u = best
        self.engaged = false
        self.caster:reset_pull()
        self.chase:reset()
        if (util.now() - self.last_log_at) > 2 then
          self.last_log_at = util.now()
          logf(
            "grind engage entry=%s d=%.1f hp=%s/%s",
            tostring(best.entry),
            best.distance or -1,
            tostring(best.health),
            tostring(best.max_health)
          )
        end
      else
        if (util.now() - self.last_log_at) > 3 then
          self.last_log_at = util.now()
          if sample then
            logf(
              "grind no target (nearby=%d) sample entry=%s hp=%s/%s alive=%s d=%.1f fl=%s npc=%s fac=%s why=%s",
              n or 0,
              tostring(sample.entry),
              tostring(sample.hp),
              tostring(sample.mhp),
              tostring(sample.alive),
              sample.d or -1,
              tostring(sample.flags),
              tostring(sample.npc),
              tostring(sample.fac),
              tostring(sample.why)
            )
          else
            logf("grind no target (nearby=%d) — wander", n or 0)
          end
        end
        self.wander:step()
        return
      end
    end

    targeting.ensure_target(tgt)
    self.current = tgt
    local dist = u.distance or 999

    local in_combat = bot.in_combat and bot.in_combat()
    if not in_combat then
      if self.charge_spell and dist >= self.charge_min and dist <= self.charge_max then
        self.caster:ready_melee(tgt, true)
        if self.caster:try_cast(self.charge_spell, tgt) then
          self.engaged = true
          return
        end
      end
      if dist > self.melee_chase then
        self.chase:to_unit(u)
        return
      end
    end

    self:combat(tgt, u)
  end

  return self
end

return M
