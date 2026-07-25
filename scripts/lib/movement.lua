-- scripts/lib/movement.lua — sticky chase / wander (avoids repath thrash)
local util = dofile("scripts/lib/util.lua")

local M = {}

function M.new_chase(opts)
  opts = opts or {}
  local self = {
    repath_period = opts.repath_period or 1.0,
    dest_slack = opts.dest_slack or 3.5,
    min_gap = opts.min_gap or 0.35,
    -- Large jump (create finally got a real pose, or teleport) always repaths.
    jump_repath = opts.jump_repath or 8.0,
    last_at = 0,
    last_x = 0,
    last_y = 0,
    has_dest = false,
  }

  function self:to_unit(u)
    if not u or not bot.move_to then
      return false
    end
    local tx, ty, tz = u.x or 0, u.y or 0, u.z or 0
    -- Never path to a clearly unset pose (pre-position create stub).
    if tx == 0 and ty == 0 and tz == 0 then
      return false
    end
    local t = util.now()
    local moving = bot.is_moving and bot.is_moving()
    local dx, dy = tx - self.last_x, ty - self.last_y
    local dest_moved = math.sqrt(dx * dx + dy * dy)

    if self.has_dest and dest_moved >= self.jump_repath then
      -- Unit pose jumped (fresh create pos / interpolated catch-up) — retarget now.
    elseif moving and (t - self.last_at) < self.repath_period and dest_moved < self.dest_slack then
      return false
    end
    if self.has_dest and (t - self.last_at) < self.min_gap and dest_moved < self.jump_repath then
      return false
    end
    self.last_at = t
    self.last_x, self.last_y = tx, ty
    self.has_dest = true
    bot.move_to(tx, ty, tz)
    return true
  end

  function self:reset()
    self.last_at = 0
    self.has_dest = false
  end

  return self
end

function M.new_wander(opts)
  opts = opts or {}
  local self = {
    period = opts.period or 2.5,
    radius = opts.radius or 22,
    last_at = 0,
  }

  function self:reset()
    self.last_at = 0
  end

  function self:step()
    local t = util.now()
    if (t - self.last_at) < self.period then
      return false
    end
    self.last_at = t
    if not bot.get_position or not bot.move_to then
      return false
    end
    local x, y, z = bot.get_position()
    x, y, z = x or 0, y or 0, z or 0
    local a = (t * 17.13) % (2 * math.pi)
    local dist = 10 + (t * 5.3) % self.radius
    bot.move_to(x + math.cos(a) * dist, y + math.sin(a) * dist, z)
    return true
  end

  return self
end

function M.stop_if_moving()
  if bot.is_moving and bot.is_moving() and bot.stop_moving then
    bot.stop_moving()
    return true
  end
  return false
end

return M
