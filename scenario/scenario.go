package scenario

// AIBundle is a rich, serializable description of AI code and data to be
// distributed to bots (via launch or live update).
//
// It supports a primary script (Main), optional helper scripts (Helpers),
// initial data table (Data) exposed to Lua as the global "scenario_data",
// and an optional override for the tick function name.
//
// This is the leaf package shared by bot/, orchestrator/, and server/ to
// avoid import cycles. See design: "Concrete Lua Scenario Host + AIBundle Spec".
type AIBundle struct {
	// Main is the primary script content (usually containing on_tick or the
	// entry point named by TickFunc). It is DoString'd first.
	Main string `json:"main"`

	// Helpers are additional named scripts that are DoString'd after Main
	// (order is map iteration order; for determinism, callers may want to
	// linearize explicitly). Useful for factoring out common functions.
	Helpers map[string]string `json:"helpers"`

	// Data is arbitrary initial state made available to the Lua script as
	// the global table "scenario_data". Scalars, nested maps, and slices
	// are supported (see luaengine.SetTable).
	Data map[string]any `json:"data"`

	// TickFunc is the name of the Lua function to invoke each tick.
	// Defaults to "on_tick" when empty. Use this for phase-specific
	// entry points (e.g. "on_tick_phase2").
	TickFunc string `json:"tick_func"`
}

// IsEmpty reports whether the bundle carries no AI payload.
func (b AIBundle) IsEmpty() bool {
	if b.Main != "" {
		return false
	}
	if len(b.Helpers) > 0 {
		return false
	}
	if len(b.Data) > 0 {
		return false
	}
	return b.TickFunc == ""
}
