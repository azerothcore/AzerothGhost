-- scripts/lib/util.lua — shared Lua helpers for bot scripts
local M = {}

-- Wall-clock seconds. Prefer bot.now_ms (real time); os.clock is CPU-only and
-- freezes cooldowns/settle while the bot is blocked on network I/O.
function M.now()
  if bot and bot.now_ms then
    return bot.now_ms() / 1000
  end
  return os.time() + (os.clock() % 1) -- coarse fallback
end

function M.guid_str(g)
  if g == nil or g == 0 or g == "0" or g == "" then
    return "0"
  end
  return tostring(g)
end

function M.is_zero_guid(g)
  return M.guid_str(g) == "0"
end

-- 32-bit AND without depending on bitop library
function M.band(a, b)
  local r, p = 0, 1
  a = math.floor(tonumber(a) or 0) % 4294967296
  b = math.floor(tonumber(b) or 0) % 4294967296
  for _ = 0, 31 do
    if a % 2 == 1 and b % 2 == 1 then
      r = r + p
    end
    a = math.floor(a / 2)
    b = math.floor(b / 2)
    p = p * 2
  end
  return r
end

function M.rage()
  if not bot.get_power then
    return 0, 100
  end
  local cur, maxp = bot.get_power()
  cur = cur or 0
  maxp = maxp or 100
  if maxp == 0 then
    maxp = 100
  end
  return cur, maxp
end

function M.unit_hp_pct(u)
  if not u then
    return 100
  end
  local h, m = u.health or 0, u.max_health or 0
  if m <= 0 then
    return 100
  end
  return (h / m) * 100
end

function M.angle_delta(a, b)
  local d = (a or 0) - (b or 0)
  while d > math.pi do
    d = d - 2 * math.pi
  end
  while d < -math.pi do
    d = d + 2 * math.pi
  end
  return math.abs(d)
end

return M
