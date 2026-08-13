package client

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sync"
	"time"
)

// Summon / instance-reset opcodes (3.3.5a).
const (
	SmsgSummonRequest       uint16 = 0x02AB
	CmsgSummonResponse      uint16 = 0x02AC
	CmsgResetInstances      uint16 = 0x031D
	SmsgInstanceReset       uint16 = 0x031E
	SmsgInstanceResetFailed uint16 = 0x031F
	SmsgResetFailedNotify   uint16 = 0x0396
	CmsgGameObjectUse       uint16 = 0x00B1
)

// SpellSummonPlayer is EFFECT_SUMMON_PLAYER (SMSG_SUMMON_REQUEST to unitTarget).
// Fired when a summoning ritual portal completes (e.g. GO 179944 spellId).
const SpellSummonPlayer uint32 = 7720

// SpellCreateMeetingStonePortal creates GO 179944 (reqParticipants=2): initiator + one helper click.
// Misc GO entry 179944; used after initiator SetTarget(far player).
const SpellCreateMeetingStonePortal uint32 = 59782

// GameObjectMeetingStoneSummonPortal is created by SpellCreateMeetingStonePortal.
const GameObjectMeetingStoneSummonPortal uint32 = 179944

// SpellRitualOfSummoning is the warlock channel that creates GO 194108 (needs 2 helper clicks).
const SpellWarlockRitualOfSummoning uint32 = 698

// Deprecated alias — prefer SpellSummonPlayer for the EffectSummonPlayer spell.
const SpellRitualOfSummoning = SpellSummonPlayer

// GameObjectUse sends CMSG_GAMEOBJ_USE for guid (uint64).
func (w *WorldClient) GameObjectUse(guid uint64) error {
	if guid == 0 {
		return fmt.Errorf("GameObjectUse: guid is 0")
	}
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, guid)
	return w.sendPacket(CmsgGameObjectUse, buf.Bytes())
}

// SummonRequest is a pending SMSG_SUMMON_REQUEST.
type SummonRequest struct {
	SummonerGUID uint64
	ZoneID       uint32
	AutoDecline  uint32 // ms until auto-decline
	ReceivedAt   time.Time
}

// SummonResponse sends CMSG_SUMMON_RESPONSE (summoner GUID + agree bool).
func (w *WorldClient) SummonResponse(summonerGUID uint64, agree bool) error {
	if summonerGUID == 0 {
		return fmt.Errorf("SummonResponse: summoner GUID is 0")
	}
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, summonerGUID)
	var a uint8
	if agree {
		a = 1
	}
	_ = binary.Write(buf, binary.LittleEndian, a)
	return w.sendPacket(CmsgSummonResponse, buf.Bytes())
}

// ResetInstances sends empty CMSG_RESET_INSTANCES (leader or solo).
func (w *WorldClient) ResetInstances() error {
	return w.sendPacket(CmsgResetInstances, nil)
}

// PendingSummon returns the last SMSG_SUMMON_REQUEST snapshot (zero if none).
func (w *WorldClient) PendingSummon() SummonRequest {
	w.summonMu.RLock()
	defer w.summonMu.RUnlock()
	return w.pendingSummon
}

// ClearPendingSummon drops the cached summon request (after accept/decline).
func (w *WorldClient) ClearPendingSummon() {
	w.summonMu.Lock()
	w.pendingSummon = SummonRequest{}
	w.summonMu.Unlock()
}

// HasPendingSummon reports a non-zero summoner GUID in cache.
func (w *WorldClient) HasPendingSummon() bool {
	return w.PendingSummon().SummonerGUID != 0
}

// LastInstanceResetMap is the map id from the last SMSG_INSTANCE_RESET (0 if none/failed).
func (w *WorldClient) LastInstanceResetMap() uint32 {
	w.summonMu.RLock()
	defer w.summonMu.RUnlock()
	return w.lastInstanceResetMap
}

// handleSummonRequest parses SMSG_SUMMON_REQUEST: guid + zoneId + autoDeclineMs.
func (w *WorldClient) handleSummonRequest(data []byte) {
	if len(data) < 16 {
		return
	}
	r := bytes.NewReader(data)
	var req SummonRequest
	_ = binary.Read(r, binary.LittleEndian, &req.SummonerGUID)
	_ = binary.Read(r, binary.LittleEndian, &req.ZoneID)
	_ = binary.Read(r, binary.LittleEndian, &req.AutoDecline)
	req.ReceivedAt = time.Now()

	w.summonMu.Lock()
	w.pendingSummon = req
	w.summonMu.Unlock()

	w.cbMu.RLock()
	hooks := append([]summonRequestHook(nil), w.summonRequestHooks...)
	legacy := w.OnSummonRequest
	w.cbMu.RUnlock()

	w.log("SMSG_SUMMON_REQUEST summoner=0x%X zone=%d autoDeclineMs=%d", req.SummonerGUID, req.ZoneID, req.AutoDecline)
	for _, h := range hooks {
		h.fn(req)
	}
	if legacy != nil {
		legacy(req)
	}
}

// handleInstanceReset parses SMSG_INSTANCE_RESET: uint32 mapId.
func (w *WorldClient) handleInstanceReset(data []byte) {
	if len(data) < 4 {
		return
	}
	mapID := binary.LittleEndian.Uint32(data[:4])
	w.summonMu.Lock()
	w.lastInstanceResetMap = mapID
	w.summonMu.Unlock()
	w.cbMu.RLock()
	hooks := append([]instanceResetHook(nil), w.instanceResetHooks...)
	w.cbMu.RUnlock()
	w.log("SMSG_INSTANCE_RESET map=%d", mapID)
	for _, h := range hooks {
		h.fn(mapID, true)
	}
}

// handleInstanceResetFailed parses SMSG_INSTANCE_RESET_FAILED (map + reason).
func (w *WorldClient) handleInstanceResetFailed(data []byte) {
	var mapID uint32
	if len(data) >= 4 {
		mapID = binary.LittleEndian.Uint32(data[:4])
	}
	w.cbMu.RLock()
	hooks := append([]instanceResetHook(nil), w.instanceResetHooks...)
	w.cbMu.RUnlock()
	w.log("SMSG_INSTANCE_RESET_FAILED map=%d", mapID)
	for _, h := range hooks {
		h.fn(mapID, false)
	}
}

// --- hooks ---

type summonRequestHook struct {
	id HookID
	fn func(SummonRequest)
}

type instanceResetHook struct {
	id HookID
	fn func(mapID uint32, ok bool)
}

// AddSummonRequestHook registers a multi-subscriber SMSG_SUMMON_REQUEST listener.
func (w *WorldClient) AddSummonRequestHook(fn func(SummonRequest)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.summonRequestHooks = append(w.summonRequestHooks, summonRequestHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.summonRequestHooks[:0]
			for _, h := range w.summonRequestHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.summonRequestHooks = out
			w.cbMu.Unlock()
		})
	}
}

// AddInstanceResetHook fires on SMSG_INSTANCE_RESET (ok=true) or FAILED (ok=false).
func (w *WorldClient) AddInstanceResetHook(fn func(mapID uint32, ok bool)) (cancel func()) {
	if fn == nil {
		return func() {}
	}
	w.cbMu.Lock()
	id := w.nextHookID()
	w.instanceResetHooks = append(w.instanceResetHooks, instanceResetHook{id: id, fn: fn})
	w.cbMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.cbMu.Lock()
			out := w.instanceResetHooks[:0]
			for _, h := range w.instanceResetHooks {
				if h.id != id {
					out = append(out, h)
				}
			}
			w.instanceResetHooks = out
			w.cbMu.Unlock()
		})
	}
}

// WaitSummonRequest waits for a SMSG_SUMMON_REQUEST (arms hook then polls cache).
// Prefer ArmSummonRequest before the action that generates the packet.
func (w *WorldClient) WaitSummonRequest(timeout time.Duration) (SummonRequest, error) {
	wait, cancel := w.ArmSummonRequest()
	defer cancel()
	return wait(timeout)
}

// ArmSummonRequest arms before the summon-generating action (Arm → cast → Wait).
func (w *WorldClient) ArmSummonRequest() (wait func(timeout time.Duration) (SummonRequest, error), cancel func()) {
	ch := make(chan SummonRequest, 1)
	// Deliver any already-pending request immediately to the waiter path.
	if p := w.PendingSummon(); p.SummonerGUID != 0 {
		select {
		case ch <- p:
		default:
		}
	}
	cancel = w.AddSummonRequestHook(func(req SummonRequest) {
		select {
		case ch <- req:
		default:
		}
	})
	wait = func(timeout time.Duration) (SummonRequest, error) {
		if timeout <= 0 {
			timeout = 15 * time.Second
		}
		select {
		case req := <-ch:
			return req, nil
		case <-time.After(timeout):
			return SummonRequest{}, fmt.Errorf("WaitSummonRequest: no SMSG_SUMMON_REQUEST within %s", timeout)
		}
	}
	return wait, cancel
}
