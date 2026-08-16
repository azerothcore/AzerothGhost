//go:build e2e

package examples_test

import (
	"testing"

	_ "github.com/go-sql-driver/mysql"

	"github.com/azerothcore/AzerothGhost/e2e/e2eharness"
)

// Example: buy a guild charter via the tabard designer (Session-style guild path).
// Pattern from the in-repo guild charter suite (BuyGuildCharter helper).
//
//	go test -tags=e2e ./e2e/examples -run TestExample_GuildCharterBuy -count=1 -v -timeout 10m
func TestExample_GuildCharterBuy(t *testing.T) {
	t.Parallel()

	authDB, charDB := e2eharness.OpenTestDBs(t)
	idents := e2eharness.MakeBotIdents("ExGBuy", 1)
	e2eharness.EnsureBotAccounts(t, authDB, idents)
	sessions := e2eharness.LoginAllianceBots(t, idents)
	e2eharness.CleanupSessionsGuildState(t, charDB, sessions)

	leader := sessions[0]
	guildName := e2eharness.UniqueGuildName("ExBuy")
	buy := e2eharness.BuyGuildCharter(t, leader, charDB, guildName)

	if buy.PetitionItemGUID == 0 || buy.ItemLow == 0 {
		e2eharness.HarnessFailf(t, "missing petition identity guid=0x%X low=%d",
			buy.PetitionItemGUID, buy.ItemLow)
	}
	if buy.ItemPush == nil {
		e2eharness.HarnessFailf(t, "expected SMSG_ITEM_PUSH_RESULT for charter")
	}
	if buy.ItemPush.Entry != e2eharness.ItemGuildCharter {
		e2eharness.HarnessFailf(t, "item push entry=%d want %d",
			buy.ItemPush.Entry, e2eharness.ItemGuildCharter)
	}
	if buy.ShowEmpty == nil || len(buy.ShowEmpty.SignatoryGUIDs) != 0 {
		e2eharness.HarnessFailf(t, "expected empty signatures on fresh charter: %+v", buy.ShowEmpty)
	}
	t.Logf("PASS charter buy petition=0x%X bag=%d slot=%d",
		buy.PetitionItemGUID, buy.ItemPush.BagSlot, buy.ItemPush.ItemSlot)
}
