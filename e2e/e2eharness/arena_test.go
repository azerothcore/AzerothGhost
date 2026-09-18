package e2eharness

import (
	"testing"
	"time"

	"github.com/azerothcore/AzerothGhost/client"
)

func TestTryWaitBattlefieldStatusEachWaitsTogether(t *testing.T) {
	bots := make([]*ScenarioBot, 3)
	for i := range bots {
		bots[i] = &ScenarioBot{Session: &Session{World: client.NewWorldClient("bot", nil, nil)}}
	}

	const timeout = 300 * time.Millisecond
	start := time.Now()
	statuses, oks := TryWaitBattlefieldStatusEach(bots, client.BattlegroundStatusWaitJoin, timeout)
	elapsed := time.Since(start)

	if len(statuses) != len(bots) || len(oks) != len(bots) {
		t.Fatalf("got %d statuses and %d oks for %d bots", len(statuses), len(oks), len(bots))
	}
	for i, ok := range oks {
		if ok {
			t.Fatalf("bot %d got a status it never received: %+v", i, statuses[i])
		}
	}
	if elapsed >= 2*timeout {
		t.Fatalf("%d bots at a %s timeout took %s, so they were waited on in turn", len(bots), timeout, elapsed)
	}
}

func TestLastQueued(t *testing.T) {
	queue := client.BattlefieldStatus{HasBattleground: true, Status: client.BattlegroundStatusWaitQueue}
	join := client.BattlefieldStatus{HasBattleground: true, Status: client.BattlegroundStatusWaitJoin}
	none := client.BattlefieldStatus{}
	other := client.BattlefieldStatus{QueueSlot: 1, HasBattleground: true, Status: client.BattlegroundStatusWaitQueue}
	noneOther := client.BattlefieldStatus{QueueSlot: 1}

	for _, tc := range []struct {
		name     string
		statuses []client.BattlefieldStatus
		want     client.BattlefieldStatus
		ok       bool
	}{
		{"never queued", nil, none, false},
		{"waiting", []client.BattlefieldStatus{queue}, queue, true},
		{"invited", []client.BattlefieldStatus{queue, join}, join, true},
		{"left", []client.BattlefieldStatus{queue, join, none}, none, false},
		{"queued again", []client.BattlefieldStatus{queue, none, queue}, queue, true},
		{"left one of two", []client.BattlefieldStatus{queue, other, none}, other, true},
		{"left the other of two", []client.BattlefieldStatus{queue, other, noneOther}, queue, true},
		{"left both", []client.BattlefieldStatus{queue, other, none, noneOther}, none, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := lastQueued(tc.statuses)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("got %+v ok=%v, want %+v ok=%v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
