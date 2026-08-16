//go:build e2e

package examples_test

import (
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/azerothcore/AzerothGhost/e2e/e2eharness"
)

// Example: GM visibility flag survives logout/login.
// Pattern from AC #25793 (.gm visible off → relog → characters.extra_flags).
//
//	go test -tags=e2e ./e2e/examples -run TestExample_GMVisibilitySurvivesRelog -count=1 -v -timeout 10m
func TestExample_GMVisibilitySurvivesRelog(t *testing.T) {
	t.Parallel()
	bot := e2eharness.NewSolo(t, e2eharness.ScenarioOpts{
		Prefix: "ExRelog",
		Level:  10,
	})

	const playerExtraGMInvisible = uint16(0x0010)

	bot.GM(t, ".gm visible off")
	bot.Save(t)

	var flags uint16
	if err := bot.CharDB.QueryRow(
		`SELECT extra_flags FROM characters WHERE guid=?`, bot.GUID,
	).Scan(&flags); err != nil {
		e2eharness.HarnessFailf(t, "read extra_flags: %v", err)
	}
	if flags&playerExtraGMInvisible == 0 {
		e2eharness.Preconditionf(t, "after .gm visible off, extra_flags=0x%X missing bit 0x10", flags)
	}

	guid := bot.GUID
	bot.Relog(t)
	time.Sleep(500 * time.Millisecond) // save-on-logout settle

	queryGUID := bot.GUID
	if queryGUID == 0 {
		queryGUID = guid
	}
	var after uint16
	err := bot.CharDB.QueryRow(
		`SELECT extra_flags FROM characters WHERE guid=?`, queryGUID,
	).Scan(&after)
	if err != nil {
		err = bot.CharDB.QueryRow(
			`SELECT extra_flags FROM characters WHERE guid=?`, guid,
		).Scan(&after)
	}
	if err != nil {
		e2eharness.HarnessFailf(t, "read extra_flags after relog: %v", err)
	}
	if after&playerExtraGMInvisible == 0 {
		e2eharness.ConfirmedBugf(t, 25793, "GM invisible did not stick after relog (extra_flags=0x%X)", after)
	}
	t.Logf("PASS GM visibility survived relog (extra_flags=0x%X)", after)
}
