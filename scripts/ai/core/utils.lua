-- scripts/ai/core/utils.lua
-- Small utilities used by engine, strategies, values. Follows existing lib style.

local M = {}

function M.clamp(v, minv, maxv)
  if v < minv then return minv end
  if v > maxv then return maxv end
  return v
end

function M.safe_div(a, b)
  if not b or b == 0 then return 0 end
  return a / b
end

function M.log_decision(msg)
  -- IMPORTANT (see AZEROTHGHOST_E2E_QUALITY_ASSURANCE_PLAN.md "Performance Isolation"):
  -- This is a hot path in many strategies. The Go side (bot.Log) must early-return with
  -- almost zero cost when LogDecisionsToChat / ValidationMode are false.
  -- Do not add expensive work here unconditionally.
  if bot and bot.log then
    bot.log("[ai] " .. tostring(msg))
  end
end

function M.dist(x1,y1,z1, x2,y2,z2)
  local dx = (x1 or 0) - (x2 or 0)
  local dy = (y1 or 0) - (y2 or 0)
  local dz = (z1 or 0) - (z2 or 0)
  return math.sqrt(dx*dx + dy*dy + dz*dz)
end

-- Target GUID helpers (get_target() returns string for 64-bit safety, but some code uses 0)
function M.is_valid_target(t)
  if not t then return false end
  if t == 0 or t == "0" or t == "" then return false end
  local n = tonumber(t)
  return n and n ~= 0
end

function M.as_target_guid(t)
  if not M.is_valid_target(t) then return "0" end
  return tostring(t)
end

return M
