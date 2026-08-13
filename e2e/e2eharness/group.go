package e2eharness

import (
	"strings"
	"testing"
	"time"

	"github.com/walkline/AzerothGhost/client"
)

// DefaultGroupTimeout is used by invite/list waiters when timeout <= 0.
const DefaultGroupTimeout = 10 * time.Second

// Invite sends CMSG_GROUP_INVITE for member.Name from the inviting bot.
func (b *ScenarioBot) Invite(t *testing.T, member *ScenarioBot) {
	t.Helper()
	if member == nil || member.Name == "" {
		HarnessFailf(t, "Invite: member name empty")
	}
	if err := b.World.GroupInvite(member.Name); err != nil {
		t.Fatalf("GroupInvite(%s): %v", member.Name, err)
	}
	t.Logf("%s invited %s", b.Name, member.Name)
}

// ArmGroupInvite arms SMSG_GROUP_INVITE before Invite (Arm → Invite → wait).
// Prefer over WaitGroupInvite, which only arms after the call and can miss a fast packet.
func (b *ScenarioBot) ArmGroupInvite() (wait func(timeout time.Duration) (string, bool), cancel func()) {
	ch := make(chan string, 1)
	cancel = b.World.AddGroupInviteHook(func(inviter string, already bool) {
		select {
		case ch <- inviter:
		default:
		}
	})
	wait = func(timeout time.Duration) (string, bool) {
		if timeout <= 0 {
			timeout = DefaultGroupTimeout
		}
		select {
		case name := <-ch:
			return name, true
		case <-time.After(timeout):
			return "", false
		}
	}
	return wait, cancel
}

// WaitGroupInvite waits for SMSG_GROUP_INVITE; returns inviter name.
// Prefer ArmGroupInvite before Invite — this helper arms only after the call.
func (b *ScenarioBot) WaitGroupInvite(t *testing.T, timeout time.Duration) string {
	t.Helper()
	wait, cancel := b.ArmGroupInvite()
	defer cancel()
	name, ok := wait(timeout)
	if !ok {
		HarnessFailf(t, "%s: timeout waiting SMSG_GROUP_INVITE within %s", b.Name, timeout)
		return ""
	}
	t.Logf("%s got group invite from %s", b.Name, name)
	return name
}

// AcceptGroup sends CMSG_GROUP_ACCEPT.
func (b *ScenarioBot) AcceptGroup(t *testing.T) {
	t.Helper()
	if err := b.World.GroupAccept(); err != nil {
		t.Fatalf("GroupAccept: %v", err)
	}
}

// DeclineGroup sends CMSG_GROUP_DECLINE.
func (b *ScenarioBot) DeclineGroup(t *testing.T) {
	t.Helper()
	if err := b.World.GroupDecline(); err != nil {
		t.Fatalf("GroupDecline: %v", err)
	}
}

// ArmGroupDecline arms SMSG_GROUP_DECLINE on the inviting leader (sent when invitee declines).
// Arm on the leader before invitee.DeclineGroup, then wait so GetGroupInvite is cleared
// server-side before the next Invite (WaitNotInGroup alone is insufficient — invite ≠ membership).
func (b *ScenarioBot) ArmGroupDecline() (wait func(timeout time.Duration) (string, bool), cancel func()) {
	ch := make(chan string, 1)
	cancel = b.World.AddGroupDeclineHook(func(name string) {
		select {
		case ch <- name:
		default:
		}
	})
	wait = func(timeout time.Duration) (string, bool) {
		if timeout <= 0 {
			timeout = DefaultGroupTimeout
		}
		select {
		case name := <-ch:
			return name, true
		case <-time.After(timeout):
			return "", false
		}
	}
	return wait, cancel
}

// DeclineGroupFrom waits for the inviting leader to observe SMSG_GROUP_DECLINE after b declines.
func (b *ScenarioBot) DeclineGroupFrom(t *testing.T, leader *ScenarioBot) {
	t.Helper()
	if leader == nil {
		b.DeclineGroup(t)
		return
	}
	waitDecl, cancelDecl := leader.ArmGroupDecline()
	defer cancelDecl()
	b.DeclineGroup(t)
	if name, ok := waitDecl(DefaultGroupTimeout); !ok {
		// Soft: decline may have raced a prior cleanup; still allow callers to proceed with retries.
		t.Logf("%s DeclineGroupFrom: no SMSG_GROUP_DECLINE on %s within %s", b.Name, leader.Name, DefaultGroupTimeout)
		return
	} else {
		t.Logf("%s declined; leader %s got SMSG_GROUP_DECLINE (%s)", b.Name, leader.Name, name)
	}
}

// LeaveGroup sends CMSG_GROUP_DISBAND (leave party on 3.3.5a).
func (b *ScenarioBot) LeaveGroup(t *testing.T) {
	t.Helper()
	if err := b.World.GroupLeave(); err != nil {
		t.Fatalf("GroupLeave: %v", err)
	}
}

// DisbandGroup is LeaveGroup for the leader path (same opcode; dissolves when rules say so).
func (b *ScenarioBot) DisbandGroup(t *testing.T) {
	t.Helper()
	b.LeaveGroup(t)
}

// SetLeader promotes member via CMSG_GROUP_SET_LEADER.
func (b *ScenarioBot) SetLeader(t *testing.T, member *ScenarioBot) {
	t.Helper()
	if member == nil || member.GUID == 0 {
		HarnessFailf(t, "SetLeader: member GUID empty")
	}
	if err := b.World.GroupSetLeader(member.GUID); err != nil {
		t.Fatalf("GroupSetLeader: %v", err)
	}
}

// Uninvite kicks or cancels invite by character name.
func (b *ScenarioBot) Uninvite(t *testing.T, memberName string) {
	t.Helper()
	if err := b.World.GroupUninvite(memberName); err != nil {
		t.Fatalf("GroupUninvite(%s): %v", memberName, err)
	}
}

// SetLootMethod sets party loot method (leader). See client.LootMethod* constants.
// threshold must be >= client.ItemQualityUncommon (2); AC silently rejects lower values
// (method may not apply). Prefer e2eharness.LootThresholdUncommon.
func (b *ScenarioBot) SetLootMethod(t *testing.T, method uint8, lootMaster uint64, threshold uint8) {
	t.Helper()
	if threshold < LootThresholdUncommon {
		t.Logf("SetLootMethod: threshold=%d below Uncommon(%d); AC may reject — use LootThresholdUncommon",
			threshold, LootThresholdUncommon)
	}
	if err := b.World.SetLootMethod(method, lootMaster, threshold); err != nil {
		t.Fatalf("SetLootMethod: %v", err)
	}
}

// InGroup reports last SMSG_GROUP_LIST membership.
func (b *ScenarioBot) InGroup() bool {
	return b.World.InGroup()
}

// IsGroupLeader reports whether this bot is the party leader (from group list).
func (b *ScenarioBot) IsGroupLeader() bool {
	return b.World.IsGroupLeader()
}

// GroupMembers returns other party members from the last list (not including self).
func (b *ScenarioBot) GroupMembers() []client.GroupMember {
	return b.World.GroupMembers()
}

// GroupState returns the last SMSG_GROUP_LIST snapshot.
func (b *ScenarioBot) GroupState() client.GroupState {
	return b.World.GroupState()
}

// WaitGroupList waits until InGroup matches wantInGroup, or MemberCount >= minMembers when minMembers>0.
func (b *ScenarioBot) WaitGroupList(t *testing.T, wantInGroup bool, minMembers int, timeout time.Duration) client.GroupState {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultGroupTimeout
	}
	deadline := time.Now().Add(timeout)
	var last client.GroupState
	for time.Now().Before(deadline) {
		last = b.World.GroupState()
		ok := last.InGroup == wantInGroup
		if wantInGroup && minMembers > 0 && last.MemberCount < minMembers {
			ok = false
		}
		if ok {
			t.Logf("%s group list: in=%v size=%d leader=0x%X", b.Name, last.InGroup, last.MemberCount, last.LeaderGUID)
			return last
		}
		time.Sleep(40 * time.Millisecond)
	}
	HarnessFailf(t, "%s: WaitGroupList wantIn=%v minMembers=%d last in=%v size=%d within %s",
		b.Name, wantInGroup, minMembers, last.InGroup, last.MemberCount, timeout)
	return last
}

// WaitNotInGroup waits until the bot is out of a party.
func (b *ScenarioBot) WaitNotInGroup(t *testing.T, timeout time.Duration) {
	t.Helper()
	b.WaitGroupList(t, false, 0, timeout)
}

// FormParty invites each member and has them accept; waits for leader roster size.
// All bots should be same faction and co-located. Leader is inviter.
// Retries invite under pad load (invite can be dropped if name lookup races post-tele).
func FormParty(t *testing.T, leader *ScenarioBot, members ...*ScenarioBot) {
	t.Helper()
	if leader == nil {
		HarnessFailf(t, "FormParty: leader nil")
	}
	if len(members) == 0 {
		HarnessFailf(t, "FormParty: need at least one member")
	}
	// Quiet both bots — combat thrash should not block invites, but combatstop is cheap.
	MustGM(t, leader.World, ".combatstop")
	for _, m := range members {
		if m == nil {
			HarnessFailf(t, "FormParty: nil member")
		}
		MustGM(t, m.World, ".combatstop")
		if !leader.World.IsInWorld() || !m.World.IsInWorld() {
			HarnessFailf(t, "FormParty: not InWorld leader=%v member=%v", leader.World.IsInWorld(), m.World.IsInWorld())
		}

		gotInvite := false
		var inviterName string
		for attempt := 0; attempt < 4 && !gotInvite; attempt++ {
			// Arm invite hook before CMSG_GROUP_INVITE (no race with packet).
			invCh := make(chan string, 1)
			cancelInvite := m.World.AddGroupInviteHook(func(inviter string, already bool) {
				select {
				case invCh <- inviter:
				default:
				}
			})
			// Ensure member not stuck with a stale pending invite.
			if attempt > 0 {
				_ = m.World.GroupDecline()
				time.Sleep(100 * time.Millisecond)
			}
			leader.Invite(t, m)
			select {
			case name := <-invCh:
				inviterName = name
				gotInvite = true
				t.Logf("%s got invite from %s (attempt %d)", m.Name, name, attempt+1)
			case <-time.After(DefaultGroupTimeout):
				t.Logf("FormParty: %s no SMSG_GROUP_INVITE attempt %d/%d (name=%q)", m.Name, attempt+1, 4, m.Name)
			}
			cancelInvite()
		}
		if !gotInvite {
			HarnessFailf(t, "FormParty: %s no SMSG_GROUP_INVITE within %s (4 attempts; name=%q)",
				m.Name, DefaultGroupTimeout, m.Name)
		}
		_ = inviterName
		m.AcceptGroup(t)
		// Wait until this member is grouped before inviting the next (stable roster growth).
		m.WaitGroupList(t, true, 2, DefaultGroupTimeout)
	}
	want := 1 + len(members)
	leader.WaitGroupList(t, true, want, DefaultGroupTimeout)
	t.Logf("FormParty: leader=%s size>=%d members=%s", leader.Name, want, memberNames(members))
}

// FormPartyAtPad teleports leader+members to pad then FormParty.
// Common multi-bot setup for group/loot/threat tests.
func FormPartyAtPad(t *testing.T, pad Position3, leader *ScenarioBot, members ...*ScenarioBot) {
	t.Helper()
	if leader == nil {
		HarnessFailf(t, "FormPartyAtPad: leader nil")
	}
	bots := make([]*ScenarioBot, 0, 1+len(members))
	bots = append(bots, leader)
	bots = append(bots, members...)
	TeleportAllPad(t, bots, pad)
	// Ensure gameplay phase after .go before CMSG_GROUP_INVITE (invite can drop mid-tele).
	for _, b := range bots {
		if b == nil {
			continue
		}
		if err := b.World.WaitForSessionPhase(client.PhaseInWorld, 8*time.Second); err != nil {
			t.Logf("FormPartyAtPad: %s WaitInWorld: %v", b.Name, err)
		}
	}
	FormParty(t, leader, members...)
}

// WaitIsGroupLeader waits until IsGroupLeader is true (after SetLeader transfer).
func (b *ScenarioBot) WaitIsGroupLeader(t *testing.T, timeout time.Duration) {
	t.Helper()
	if timeout <= 0 {
		timeout = DefaultGroupTimeout
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b.IsGroupLeader() {
			t.Logf("%s is group leader", b.Name)
			return
		}
		time.Sleep(40 * time.Millisecond)
	}
	HarnessFailf(t, "%s: not group leader within %s (InGroup=%v)", b.Name, timeout, b.InGroup())
}

func memberNames(members []*ScenarioBot) string {
	parts := make([]string, 0, len(members))
	for _, m := range members {
		if m != nil {
			parts = append(parts, m.Name)
		}
	}
	return strings.Join(parts, ",")
}

// DisbandParty has every bot leave (best-effort) so leftover groups don't leak across tests.
func DisbandParty(t *testing.T, bots ...*ScenarioBot) {
	t.Helper()
	for _, b := range bots {
		if b == nil || b.World == nil {
			continue
		}
		if b.InGroup() {
			b.LeaveGroup(t)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		allOut := true
		for _, b := range bots {
			if b != nil && b.InGroup() {
				allOut = false
				break
			}
		}
		if allOut {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, b := range bots {
		if b != nil && b.InGroup() {
			t.Logf("WARNING: %s still InGroup after DisbandParty", b.Name)
		}
	}
}
