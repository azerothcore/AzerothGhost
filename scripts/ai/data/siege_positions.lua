-- scripts/ai/data/siege_positions.lua
-- Named locations and faction start points for Orgrimmar Siege scenario.
-- Coords are approximate; validate/adjust on live server with /loc or GM .gps
-- Map 1 = Kalimdor / Durotar / Orgrimmar area.

local M = {}

-- Main battle area near Orgrimmar gate (as specified for reliable arrival regardless of race start zone)
-- All bots should be teleported here (with jitter) instead of pathing from Teldrassil/Dun Morogh etc.
M.ORGRIMMAR_GATE = { map = 1, x = 1368, y = -4373, z = 26.057, o = 0, desc = "Orgrimmar main gate / Durotar entrance area" }

-- Faction rally / staging areas (clustered around the gate with slight bias)
M.ALLIANCE_STAGING = { map=1, x=1355, y=-4385, z=26, o=0, desc="Alliance staging slightly outside main gate" }
M.HORDE_STAGING    = { map=1, x=1380, y=-4360, z=26, o=0, desc="Horde staging slightly inside / near gate" }

-- Key chokepoints and tactical spots (inside Orgrimmar)
M.POSITIONS = {
  main_gate_outer = { map=1, x=1380, y=-4370, z=25, o=0, desc="Main gate exterior (Alliance assault start)" },
  main_gate_inner = { map=1, x=1550, y=-4420, z=20, o=0, desc="Main gate interior / Valley entrance" },
  valley_strength = { map=1, x=1630, y=-4400, z=18, o=0, desc="Valley of Strength (central hub)" },
  drag_entrance   = { map=1, x=1750, y=-4400, z=20, o=0, desc="The Drag entrance" },
  upper_ramp      = { map=1, x=1680, y=-4350, z=30, o=0, desc="Ramps to upper districts" },
  bank_area       = { map=1, x=1620, y=-4450, z=18, o=0, desc="Bank / auction vicinity (defensible)" },
  flight_master   = { map=1, x=1580, y=-4300, z=25, o=0, desc="Flight master area (healer fallback)" },
}

-- Helper to pick start based on faction ("alliance" or "horde")
function M.get_start_pos(faction)
  faction = string.lower(faction or "horde")
  if faction == "alliance" or faction == "a" then
    return M.ALLIANCE_STAGING
  end
  return M.HORDE_STAGING
end

-- Random jitter around a base pos for spread (simple). Use larger spread so they don't stack.
function M.jitter(pos, amount)
  amount = amount or 15
  local ox = (math.random() * 2 - 1) * amount
  local oy = (math.random() * 2 - 1) * amount
  return {
    map = pos.map or 1,
    x = pos.x + ox,
    y = pos.y + oy,
    z = pos.z or 26,
    o = pos.o or (math.random() * 6.28)
  }
end

-- Convenience: get a spread out point around the main Orgrimmar gate
function M.get_orgrimmar_gate_spread()
  return M.jitter(M.ORGRIMMAR_GATE, 18)
end

return M
