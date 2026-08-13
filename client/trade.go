package client

import (
	"bytes"
	"encoding/binary"
	"fmt"
)

// Trade opcodes (3.3.5a / AzerothCore Opcodes.h).
const (
	CmsgInitiateTrade  uint16 = 0x0116
	CmsgBeginTrade     uint16 = 0x0117
	CmsgBusyTrade      uint16 = 0x0118
	CmsgIgnoreTrade    uint16 = 0x0119
	CmsgAcceptTrade    uint16 = 0x011A
	CmsgUnacceptTrade  uint16 = 0x011B
	CmsgCancelTrade    uint16 = 0x011C
	CmsgSetTradeItem   uint16 = 0x011D
	CmsgClearTradeItem uint16 = 0x011E
	CmsgSetTradeGold   uint16 = 0x011F
	SmsgTradeStatus    uint16 = 0x0120
	SmsgTradeStatusExt uint16 = 0x0121
)

// TradeStatus codes (SharedDefines.h TradeStatus).
const (
	TradeStatusBusy          uint32 = 0
	TradeStatusBeginTrade    uint32 = 1
	TradeStatusOpenWindow    uint32 = 2
	TradeStatusTradeCanceled uint32 = 3
	TradeStatusTradeAccept   uint32 = 4
	TradeStatusBusy2         uint32 = 5
	TradeStatusNoTarget      uint32 = 6
	TradeStatusBackToTrade   uint32 = 7
	TradeStatusTradeComplete uint32 = 8
	TradeStatusTradeRejected uint32 = 9
	TradeStatusTargetTooFar  uint32 = 10
	TradeStatusWrongFaction  uint32 = 11
	TradeStatusCloseWindow   uint32 = 12
	TradeStatusIgnoreYou     uint32 = 14
	TradeStatusYouStunned    uint32 = 15
	TradeStatusTargetStunned uint32 = 16
	TradeStatusYouDead       uint32 = 17
	TradeStatusTargetDead    uint32 = 18
	TradeStatusYouLogout     uint32 = 19
	TradeStatusTargetLogout  uint32 = 20
	TradeStatusTrialAccount  uint32 = 21
	TradeStatusWrongRealm    uint32 = 22
	TradeStatusNotOnTapList  uint32 = 23
)

// Trade slot layout (TradeData.h): 0..5 traded, 6 non-traded.
const (
	TradeSlotTradedCount = 6
	TradeSlotCount       = 7
)

// TradeStatusInfo is a parsed SMSG_TRADE_STATUS.
type TradeStatusInfo struct {
	Status                     uint32
	TraderGUID                 uint64 // BEGIN_TRADE
	Result                     uint32 // CLOSE_WINDOW inventory result
	IsTargetResult             uint8
	ItemLimitedByLimitCategory uint32
	Slot                       uint8 // WRONG_REALM / NOT_ON_TAPLIST
}

// TradeStatusName returns a short label for logging.
func TradeStatusName(st uint32) string {
	switch st {
	case TradeStatusBusy:
		return "BUSY"
	case TradeStatusBeginTrade:
		return "BEGIN_TRADE"
	case TradeStatusOpenWindow:
		return "OPEN_WINDOW"
	case TradeStatusTradeCanceled:
		return "TRADE_CANCELED"
	case TradeStatusTradeAccept:
		return "TRADE_ACCEPT"
	case TradeStatusBusy2:
		return "BUSY_2"
	case TradeStatusNoTarget:
		return "NO_TARGET"
	case TradeStatusBackToTrade:
		return "BACK_TO_TRADE"
	case TradeStatusTradeComplete:
		return "TRADE_COMPLETE"
	case TradeStatusTradeRejected:
		return "TRADE_REJECTED"
	case TradeStatusTargetTooFar:
		return "TARGET_TOO_FAR"
	case TradeStatusWrongFaction:
		return "WRONG_FACTION"
	case TradeStatusCloseWindow:
		return "CLOSE_WINDOW"
	case TradeStatusIgnoreYou:
		return "IGNORE_YOU"
	case TradeStatusYouStunned:
		return "YOU_STUNNED"
	case TradeStatusTargetStunned:
		return "TARGET_STUNNED"
	case TradeStatusYouDead:
		return "YOU_DEAD"
	case TradeStatusTargetDead:
		return "TARGET_DEAD"
	case TradeStatusYouLogout:
		return "YOU_LOGOUT"
	case TradeStatusTargetLogout:
		return "TARGET_LOGOUT"
	case TradeStatusTrialAccount:
		return "TRIAL_ACCOUNT"
	case TradeStatusWrongRealm:
		return "WRONG_REALM"
	case TradeStatusNotOnTapList:
		return "NOT_ON_TAPLIST"
	default:
		return fmt.Sprintf("STATUS_%d", st)
	}
}

// ParseTradeStatus decodes SMSG_TRADE_STATUS (variable payload after status).
func ParseTradeStatus(data []byte) (TradeStatusInfo, error) {
	var info TradeStatusInfo
	if len(data) < 4 {
		return info, fmt.Errorf("SMSG_TRADE_STATUS too short: %d", len(data))
	}
	r := bytes.NewReader(data)
	if err := binary.Read(r, binary.LittleEndian, &info.Status); err != nil {
		return info, err
	}
	switch info.Status {
	case TradeStatusBeginTrade:
		if err := binary.Read(r, binary.LittleEndian, &info.TraderGUID); err != nil {
			return info, err
		}
	case TradeStatusOpenWindow:
		var tradeID uint32
		_ = binary.Read(r, binary.LittleEndian, &tradeID)
	case TradeStatusCloseWindow:
		_ = binary.Read(r, binary.LittleEndian, &info.Result)
		_ = binary.Read(r, binary.LittleEndian, &info.IsTargetResult)
		_ = binary.Read(r, binary.LittleEndian, &info.ItemLimitedByLimitCategory)
	case TradeStatusWrongRealm, TradeStatusNotOnTapList:
		_ = binary.Read(r, binary.LittleEndian, &info.Slot)
	}
	return info, nil
}

// InitiateTrade sends CMSG_INITIATE_TRADE targeting other player GUID.
func (w *WorldClient) InitiateTrade(targetGUID uint64) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, targetGUID)
	return w.sendPacket(CmsgInitiateTrade, buf.Bytes())
}

// BeginTrade sends CMSG_BEGIN_TRADE (response to BEGIN_TRADE status).
func (w *WorldClient) BeginTrade() error {
	return w.sendPacket(CmsgBeginTrade, nil)
}

// AcceptTrade sends CMSG_ACCEPT_TRADE (empty payload on 3.3.5a).
func (w *WorldClient) AcceptTrade() error {
	return w.sendPacket(CmsgAcceptTrade, nil)
}

// UnacceptTrade sends CMSG_UNACCEPT_TRADE.
func (w *WorldClient) UnacceptTrade() error {
	return w.sendPacket(CmsgUnacceptTrade, nil)
}

// CancelTrade sends CMSG_CANCEL_TRADE.
func (w *WorldClient) CancelTrade() error {
	return w.sendPacket(CmsgCancelTrade, nil)
}

// SetTradeItem puts bag/slot item into trade slot (0..5).
// bag: 255 = backpack (INVENTORY_SLOT_BAG_0), matching client item-push encoding.
func (w *WorldClient) SetTradeItem(tradeSlot, bag, invSlot uint8) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, tradeSlot)
	_ = binary.Write(buf, binary.LittleEndian, bag)
	_ = binary.Write(buf, binary.LittleEndian, invSlot)
	return w.sendPacket(CmsgSetTradeItem, buf.Bytes())
}

// ClearTradeItem clears a trade slot.
func (w *WorldClient) ClearTradeItem(tradeSlot uint8) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, tradeSlot)
	return w.sendPacket(CmsgClearTradeItem, buf.Bytes())
}

// SetTradeGold sets offered copper.
func (w *WorldClient) SetTradeGold(copper uint32) error {
	buf := new(bytes.Buffer)
	_ = binary.Write(buf, binary.LittleEndian, copper)
	return w.sendPacket(CmsgSetTradeGold, buf.Bytes())
}

func (w *WorldClient) handleTradeStatus(data []byte) {
	info, err := ParseTradeStatus(data)
	if err != nil {
		w.logAt(LogWarn, "SMSG_TRADE_STATUS parse: %v", err)
		return
	}
	w.tradeMu.Lock()
	w.lastTradeStatus = info
	w.tradeStatusSeen = true
	w.tradeStatusSeq++
	switch info.Status {
	case TradeStatusBeginTrade, TradeStatusOpenWindow, TradeStatusTradeAccept, TradeStatusBackToTrade:
		w.tradeOpen = true
	case TradeStatusTradeComplete, TradeStatusTradeCanceled, TradeStatusCloseWindow,
		TradeStatusTargetTooFar, TradeStatusTradeRejected, TradeStatusNoTarget,
		TradeStatusYouLogout, TradeStatusTargetLogout, TradeStatusYouDead, TradeStatusTargetDead,
		TradeStatusBusy, TradeStatusBusy2:
		w.tradeOpen = false
	}
	w.tradeMu.Unlock()
	// Sparse e2e flake signal — keep at Info (not per-hit combat volume).
	w.logAt(LogInfo, "SMSG_TRADE_STATUS %s", TradeStatusName(info.Status))
	w.invokeTradeStatusHooks(info)
}

// TradeOpen reports whether the client believes a trade window is active.
func (w *WorldClient) TradeOpen() bool {
	w.tradeMu.RLock()
	defer w.tradeMu.RUnlock()
	return w.tradeOpen
}

// LastTradeStatus returns the last SMSG_TRADE_STATUS snapshot.
// Note: TradeStatusBusy == 0, so an all-zero struct means "never received" unless
// TradeStatusSeen is true. Prefer TradeStatusSeen / TradeStatusSeq for that distinction.
func (w *WorldClient) LastTradeStatus() TradeStatusInfo {
	w.tradeMu.RLock()
	defer w.tradeMu.RUnlock()
	return w.lastTradeStatus
}

// TradeStatusSeen reports whether any SMSG_TRADE_STATUS was received this session.
func (w *WorldClient) TradeStatusSeen() bool {
	w.tradeMu.RLock()
	defer w.tradeMu.RUnlock()
	return w.tradeStatusSeen
}

// TradeStatusSeq is a monotonic counter of SMSG_TRADE_STATUS packets (0 = none yet).
func (w *WorldClient) TradeStatusSeq() uint64 {
	w.tradeMu.RLock()
	defer w.tradeMu.RUnlock()
	return w.tradeStatusSeq
}
