package client

import "sync"

// HookID identifies a registered multi-subscriber callback.
type HookID uint64

type packetHook struct {
	id HookID
	fn func(opcode uint16, data []byte)
}
type tradeStatusHook struct {
	id HookID
	fn func(TradeStatusInfo)
}
type lootOpenedHook struct {
	id HookID
	fn func(lootGUID uint64, items []LootItem)
}
type lootStartRollHook struct {
	id HookID
	fn func(LootStartRoll)
}
type lootRollHook struct {
	id HookID
	fn func(LootRollEvent)
}
type lootRollWonHook struct {
	id HookID
	fn func(LootRollWon)
}
type lootAllPassedHook struct {
	id HookID
	fn func(LootAllPassed)
}
type spellCastResultHook struct {
	id HookID
	fn func(spellID uint32, success bool, failReason uint8)
}
type groupInviteHook struct {
	id HookID
	fn func(inviterName string, alreadyInGroup bool)
}
type groupDeclineHook struct {
	id HookID
	fn func(declinerName string)
}
type groupListHook struct {
	id HookID
	fn func(GroupState)
}

// packet / event hook registries (race-safe fan-out).
// Legacy On* fields are still invoked after hooks for backward compatibility;
// prefer Add*Hook + cancel for new code.

// nextHookID must be called with cbMu held.
func (w *WorldClient) nextHookID() HookID {
	w.hookSeq++
	return HookID(w.hookSeq)
}

// AddPacketHook registers a raw opcode listener. Returns cancel (safe to call once).
func (w *WorldClient) AddPacketHook(fn func(opcode uint16, data []byte)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.packetHooks = append(w.packetHooks, packetHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() { w.removePacketHook(id) })
	}
}

func (w *WorldClient) removePacketHook(id HookID) {
	w.cbMu.Lock()
	defer w.cbMu.Unlock()
	out := w.packetHooks[:0]
	for _, h := range w.packetHooks {
		if h.id != id {
			out = append(out, h)
		}
	}
	w.packetHooks = out
}

func (w *WorldClient) invokePacketHooks(opcode uint16, data []byte) {
	w.cbMu.RLock()
	hooks := append([]packetHook(nil), w.packetHooks...)
	legacy := w.OnPacket
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(opcode, data)
	}
	if legacy != nil {
		legacy(opcode, data)
	}
}

// --- Trade status ---

func (w *WorldClient) AddTradeStatusHook(fn func(TradeStatusInfo)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.tradeStatusHooks = append(w.tradeStatusHooks, tradeStatusHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.tradeStatusHooks[:0]
			for _, h := range w.tradeStatusHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.tradeStatusHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeTradeStatusHooks(info TradeStatusInfo) {
	w.cbMu.RLock()
	hooks := append([]tradeStatusHook(nil), w.tradeStatusHooks...)
	legacy := w.OnTradeStatus
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(info)
	}
	if legacy != nil {
		legacy(info)
	}
}

// SetOnTradeStatus sets the legacy single OnTradeStatus under cbMu.
func (w *WorldClient) SetOnTradeStatus(fn func(TradeStatusInfo)) {
	w.cbMu.Lock()
	w.OnTradeStatus = fn
	w.cbMu.Unlock()
}

// --- Loot opened ---

func (w *WorldClient) AddLootOpenedHook(fn func(lootGUID uint64, items []LootItem)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.lootOpenedHooks = append(w.lootOpenedHooks, lootOpenedHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.lootOpenedHooks[:0]
			for _, h := range w.lootOpenedHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.lootOpenedHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeLootOpenedHooks(lootGUID uint64, items []LootItem) {
	w.cbMu.RLock()
	hooks := append([]lootOpenedHook(nil), w.lootOpenedHooks...)
	legacy := w.OnLootOpened
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(lootGUID, items)
	}
	if legacy != nil {
		legacy(lootGUID, items)
	}
}

// --- Loot start roll ---

func (w *WorldClient) AddLootStartRollHook(fn func(LootStartRoll)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.lootStartRollHooks = append(w.lootStartRollHooks, lootStartRollHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.lootStartRollHooks[:0]
			for _, h := range w.lootStartRollHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.lootStartRollHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeLootStartRollHooks(r LootStartRoll) {
	w.cbMu.RLock()
	hooks := append([]lootStartRollHook(nil), w.lootStartRollHooks...)
	legacy := w.OnLootStartRoll
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(r)
	}
	if legacy != nil {
		legacy(r)
	}
}

// --- Loot roll / won / all passed ---

func (w *WorldClient) AddLootRollHook(fn func(LootRollEvent)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.lootRollHooks = append(w.lootRollHooks, lootRollHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.lootRollHooks[:0]
			for _, h := range w.lootRollHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.lootRollHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeLootRollHooks(r LootRollEvent) {
	w.cbMu.RLock()
	hooks := append([]lootRollHook(nil), w.lootRollHooks...)
	legacy := w.OnLootRoll
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(r)
	}
	if legacy != nil {
		legacy(r)
	}
}

func (w *WorldClient) AddLootRollWonHook(fn func(LootRollWon)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.lootRollWonHooks = append(w.lootRollWonHooks, lootRollWonHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.lootRollWonHooks[:0]
			for _, h := range w.lootRollWonHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.lootRollWonHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeLootRollWonHooks(r LootRollWon) {
	w.cbMu.RLock()
	hooks := append([]lootRollWonHook(nil), w.lootRollWonHooks...)
	legacy := w.OnLootRollWon
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(r)
	}
	if legacy != nil {
		legacy(r)
	}
}

func (w *WorldClient) AddLootAllPassedHook(fn func(LootAllPassed)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.lootAllPassedHooks = append(w.lootAllPassedHooks, lootAllPassedHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.lootAllPassedHooks[:0]
			for _, h := range w.lootAllPassedHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.lootAllPassedHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeLootAllPassedHooks(r LootAllPassed) {
	w.cbMu.RLock()
	hooks := append([]lootAllPassedHook(nil), w.lootAllPassedHooks...)
	legacy := w.OnLootAllPassed
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(r)
	}
	if legacy != nil {
		legacy(r)
	}
}

// --- Spell cast result ---

func (w *WorldClient) AddSpellCastResultHook(fn func(spellID uint32, success bool, failReason uint8)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.spellCastResultHooks = append(w.spellCastResultHooks, spellCastResultHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.spellCastResultHooks[:0]
			for _, h := range w.spellCastResultHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.spellCastResultHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeSpellCastResultHooks(spellID uint32, success bool, failReason uint8) {
	w.cbMu.RLock()
	hooks := append([]spellCastResultHook(nil), w.spellCastResultHooks...)
	legacy := w.OnSpellCastResult
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(spellID, success, failReason)
	}
	if legacy != nil {
		legacy(spellID, success, failReason)
	}
}

// --- Group invite / list ---

func (w *WorldClient) AddGroupInviteHook(fn func(inviterName string, alreadyInGroup bool)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.groupInviteHooks = append(w.groupInviteHooks, groupInviteHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.groupInviteHooks[:0]
			for _, h := range w.groupInviteHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.groupInviteHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeGroupInviteHooks(name string, already bool) {
	w.cbMu.RLock()
	hooks := append([]groupInviteHook(nil), w.groupInviteHooks...)
	legacy := w.OnGroupInvite
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(name, already)
	}
	if legacy != nil {
		legacy(name, already)
	}
}

// AddGroupDeclineHook registers a listener for SMSG_GROUP_DECLINE (leader side).
func (w *WorldClient) AddGroupDeclineHook(fn func(declinerName string)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.groupDeclineHooks = append(w.groupDeclineHooks, groupDeclineHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.groupDeclineHooks[:0]
			for _, h := range w.groupDeclineHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.groupDeclineHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeGroupDeclineHooks(name string) {
	w.cbMu.RLock()
	hooks := append([]groupDeclineHook(nil), w.groupDeclineHooks...)
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(name)
	}
}

func (w *WorldClient) AddGroupListHook(fn func(GroupState)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.groupListHooks = append(w.groupListHooks, groupListHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.groupListHooks[:0]
			for _, h := range w.groupListHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.groupListHooks = out
			w.cbMu.Unlock()
		})
	}
}

func (w *WorldClient) invokeGroupListHooks(st GroupState) {
	w.cbMu.RLock()
	hooks := append([]groupListHook(nil), w.groupListHooks...)
	legacy := w.OnGroupList
	w.cbMu.RUnlock()
	for _, h := range hooks {
		h.fn(st)
	}
	if legacy != nil {
		legacy(st)
	}
}
