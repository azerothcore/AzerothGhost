package e2eharness

import (
	"testing"
	"time"

	"github.com/azerothcore/AzerothGhost/client"
)

// FlushWorld sends `.gps` and waits for SMSG_MESSAGECHAT.
//
// Same-session CMSG_MESSAGECHAT GM commands are handled in order on the
// world thread. A sys-message after `.gps` means every MustGM issued on
// this bot before this call has already been applied (.pvp on, .gm off,
// .learn, …). It does not wait for another bot's commands — call it on
// each session that queued setup.
func FlushWorld(t *testing.T, w *client.WorldClient) {
	t.Helper()
	if w == nil {
		Preconditionf(t, "FlushWorld: nil world")
	}
	got := make(chan struct{}, 1)
	cancel := w.AddPacketHook(func(op uint16, _ []byte) {
		if op != client.SmsgMessageChat {
			return
		}
		select {
		case got <- struct{}{}:
		default:
		}
	})
	defer cancel()
	MustGM(t, w, ".gps")
	select {
	case <-got:
		t.Logf("FlushWorld: world ack (SMSG_MESSAGECHAT after .gps)")
	case <-time.After(5 * time.Second):
		Preconditionf(t, "FlushWorld: no SMSG_MESSAGECHAT after .gps")
	}
}

// FlushWorld is ScenarioBot.FlushWorld.
func (b *ScenarioBot) FlushWorld(t *testing.T) {
	t.Helper()
	FlushWorld(t, b.World)
}
