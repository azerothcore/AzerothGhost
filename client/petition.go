package client

import (
	"encoding/binary"
	"fmt"
)

// PetitionSignResult codes (SMSG_PETITION_SIGN_RESULTS).
const (
	PetitionSignOK             uint32 = 0
	PetitionSignAlreadySigned  uint32 = 1
	PetitionSignAlreadyInGuild uint32 = 2
	PetitionSignCantSignOwn    uint32 = 3
)

// PetitionTurnResult codes (SMSG_TURN_IN_PETITION_RESULTS).
const (
	PetitionTurnOK                 uint32 = 0
	PetitionTurnAlreadyInGuild     uint32 = 2
	PetitionTurnNeedMoreSignatures uint32 = 4
)

// PetitionShowSignatures is the parsed SMSG_PETITION_SHOW_SIGNATURES payload.
type PetitionShowSignatures struct {
	PetitionGUID   uint64
	OwnerGUID      uint64
	PetitionID     uint32
	SignatoryGUIDs []uint64
}

// PetitionSignResults is the parsed SMSG_PETITION_SIGN_RESULTS payload.
type PetitionSignResults struct {
	PetitionGUID uint64
	PlayerGUID   uint64
	Result       uint32
}

// SendPetitionBuy buys a guild charter from a tabard-designer NPC (CMSG_PETITION_BUY).
// Packet layout mirrors AC WorldSession::HandlePetitionBuyOpcode.
func (w *WorldClient) SendPetitionBuy(npcGUID uint64, guildName string) error {
	// guidNPC(8) + skip u32 + skip u64 + name\0 + empty\0 + 7*u32 + u16 + 3*u32 + 10*empty\0 + clientIndex u32 + u32
	nameBytes := append([]byte(guildName), 0)
	empty := []byte{0}
	// Fixed-size skips after the two strings:
	// 7*uint32 + uint16 + 3*uint32 + 10 empty cstrings + 2*uint32
	fixedAfterStrings := 7*4 + 2 + 3*4 + 10*1 + 2*4
	buf := make([]byte, 0, 8+4+8+len(nameBytes)+len(empty)+fixedAfterStrings)
	tmp := make([]byte, 8)
	binary.LittleEndian.PutUint64(tmp, npcGUID)
	buf = append(buf, tmp...)
	binary.LittleEndian.PutUint32(tmp[:4], 0)
	buf = append(buf, tmp[:4]...)
	binary.LittleEndian.PutUint64(tmp, 0)
	buf = append(buf, tmp...)
	buf = append(buf, nameBytes...)
	buf = append(buf, empty...)
	for i := 0; i < 7; i++ {
		buf = append(buf, 0, 0, 0, 0)
	}
	buf = append(buf, 0, 0) // uint16
	for i := 0; i < 3; i++ {
		buf = append(buf, 0, 0, 0, 0)
	}
	for i := 0; i < 10; i++ {
		buf = append(buf, 0)
	}
	// clientIndex + trailing uint32
	buf = append(buf, 0, 0, 0, 0)
	buf = append(buf, 0, 0, 0, 0)
	return w.sendPacket(CmsgPetitionBuy, buf)
}

// SendPetitionShowSignatures requests the signature list for a charter item.
func (w *WorldClient) SendPetitionShowSignatures(petitionItemGUID uint64) error {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, petitionItemGUID)
	return w.sendPacket(CmsgPetitionShowSignatures, buf)
}

// SendPetitionSign signs a guild/arena charter for the logged-in character.
func (w *WorldClient) SendPetitionSign(petitionItemGUID uint64) error {
	buf := make([]byte, 9)
	binary.LittleEndian.PutUint64(buf[0:8], petitionItemGUID)
	buf[8] = 0 // unk
	return w.sendPacket(CmsgPetitionSign, buf)
}

// SendOfferPetition offers a charter to another online player (target receives SHOW_SIGNATURES).
func (w *WorldClient) SendOfferPetition(petitionItemGUID, targetGUID uint64) error {
	buf := make([]byte, 4+8+8)
	binary.LittleEndian.PutUint32(buf[0:4], 0) // junk
	binary.LittleEndian.PutUint64(buf[4:12], petitionItemGUID)
	binary.LittleEndian.PutUint64(buf[12:20], targetGUID)
	return w.sendPacket(CmsgOfferPetition, buf)
}

// SendTurnInPetition turns a charter in (guild creation for guild charters).
func (w *WorldClient) SendTurnInPetition(petitionItemGUID uint64) error {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, petitionItemGUID)
	return w.sendPacket(CmsgTurnInPetition, buf)
}

// SendPetitionQuery queries charter metadata.
func (w *WorldClient) SendPetitionQuery(petitionID uint32, petitionItemGUID uint64) error {
	buf := make([]byte, 4+8)
	binary.LittleEndian.PutUint32(buf[0:4], petitionID)
	binary.LittleEndian.PutUint64(buf[4:12], petitionItemGUID)
	return w.sendPacket(CmsgPetitionQuery, buf)
}

// SendPetitionDecline declines a petition offer and notifies the owner.
func (w *WorldClient) SendPetitionDecline(petitionItemGUID uint64) error {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, petitionItemGUID)
	return w.sendPacket(MsgPetitionDecline, buf)
}

// ParsePetitionShowSignatures decodes SMSG_PETITION_SHOW_SIGNATURES.
func ParsePetitionShowSignatures(data []byte) (*PetitionShowSignatures, error) {
	if len(data) < 21 {
		return nil, fmt.Errorf("SMSG_PETITION_SHOW_SIGNATURES too short: %d", len(data))
	}
	out := &PetitionShowSignatures{
		PetitionGUID: binary.LittleEndian.Uint64(data[0:8]),
		OwnerGUID:    binary.LittleEndian.Uint64(data[8:16]),
		PetitionID:   binary.LittleEndian.Uint32(data[16:20]),
	}
	count := int(data[20])
	need := 21 + count*12
	if len(data) < need {
		return nil, fmt.Errorf("SMSG_PETITION_SHOW_SIGNATURES truncated: need %d have %d", need, len(data))
	}
	out.SignatoryGUIDs = make([]uint64, 0, count)
	off := 21
	for i := 0; i < count; i++ {
		out.SignatoryGUIDs = append(out.SignatoryGUIDs, binary.LittleEndian.Uint64(data[off:off+8]))
		off += 12 // guid + uint32 pad
	}
	return out, nil
}

// ParsePetitionSignResults decodes SMSG_PETITION_SIGN_RESULTS.
func ParsePetitionSignResults(data []byte) (*PetitionSignResults, error) {
	if len(data) < 20 {
		return nil, fmt.Errorf("SMSG_PETITION_SIGN_RESULTS too short: %d", len(data))
	}
	return &PetitionSignResults{
		PetitionGUID: binary.LittleEndian.Uint64(data[0:8]),
		PlayerGUID:   binary.LittleEndian.Uint64(data[8:16]),
		Result:       binary.LittleEndian.Uint32(data[16:20]),
	}, nil
}

// ParseTurnInPetitionResults decodes SMSG_TURN_IN_PETITION_RESULTS.
func ParseTurnInPetitionResults(data []byte) (uint32, error) {
	if len(data) < 4 {
		return 0, fmt.Errorf("SMSG_TURN_IN_PETITION_RESULTS too short: %d", len(data))
	}
	return binary.LittleEndian.Uint32(data[0:4]), nil
}
