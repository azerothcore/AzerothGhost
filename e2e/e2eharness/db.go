package e2eharness

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// OpenAuthDB opens the auth database (acore_auth).
func OpenAuthDB() (*sql.DB, error) {
	db, err := sql.Open("mysql", AuthDSN)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// OpenCharDB opens the characters database (acore_characters).
func OpenCharDB() (*sql.DB, error) {
	db, err := sql.Open("mysql", CharDSN)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// EnsureAccount creates or updates an account with the given password (SRP6).
func EnsureAccount(db *sql.DB, username, password string) error {
	u := strings.ToUpper(username)
	p := strings.ToUpper(password)
	salt, verifier := ComputeSRP6(u, p)
	var id int
	err := db.QueryRow(`SELECT id FROM account WHERE username=?`, u).Scan(&id)
	if err == sql.ErrNoRows {
		_, err = db.Exec(`INSERT INTO account (username, salt, verifier, expansion) VALUES (?,?,?,2)`, u, salt, verifier)
		return err
	}
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE account SET salt=?, verifier=? WHERE username=?`, salt, verifier, u)
	return err
}

// SetGM sets GM level for an account on all realms (RealmID=-1).
func SetGM(db *sql.DB, username string, level int) error {
	u := strings.ToUpper(username)
	var id int
	if err := db.QueryRow(`SELECT id FROM account WHERE username=?`, u).Scan(&id); err != nil {
		return err
	}
	_, err := db.Exec(
		`INSERT INTO account_access (id, gmlevel, RealmID) VALUES (?,?, -1)
		 ON DUPLICATE KEY UPDATE gmlevel=VALUES(gmlevel)`,
		id, level,
	)
	return err
}

// CleanupPlayer removes guild membership and petition rows for a character guid.
func CleanupPlayer(db *sql.DB, guid uint64) error {
	if _, err := db.Exec(`DELETE FROM guild_member WHERE guid=?`, guid); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE ps FROM petition_sign ps WHERE ps.playerguid=? OR ps.ownerguid=?`, guid, guid); err != nil {
		return err
	}
	if _, err := db.Exec(`DELETE FROM petition WHERE ownerguid=?`, guid); err != nil {
		return err
	}
	return nil
}

// CleanupGuildIfEmpty deletes guild rows when the guild has no members left
// (and always removes bank state for the guild if leader matches).
func CleanupGuildForLeader(db *sql.DB, leaderGUID uint64) {
	var guildID uint32
	if err := db.QueryRow(`SELECT guildid FROM guild WHERE leaderguid=?`, leaderGUID).Scan(&guildID); err != nil {
		return
	}
	_, _ = db.Exec(`DELETE FROM guild_bank_item WHERE guildid=?`, guildID)
	_, _ = db.Exec(`DELETE FROM guild_bank_right WHERE guildid=?`, guildID)
	_, _ = db.Exec(`DELETE FROM guild_bank_tab WHERE guildid=?`, guildID)
	_, _ = db.Exec(`DELETE FROM guild_bank_eventlog WHERE guildid=?`, guildID)
	_, _ = db.Exec(`DELETE FROM guild_eventlog WHERE guildid=?`, guildID)
	_, _ = db.Exec(`DELETE FROM guild_member_withdraw WHERE guid IN (SELECT guid FROM guild_member WHERE guildid=?)`, guildID)
	_, _ = db.Exec(`DELETE FROM guild_member WHERE guildid=?`, guildID)
	_, _ = db.Exec(`DELETE FROM guild_rank WHERE guildid=?`, guildID)
	_, _ = db.Exec(`DELETE FROM guild WHERE guildid=?`, guildID)
}

// SeedGuildCharter inserts a guild charter item + petition row for ownerGUID.
// The owner must be offline (or re-login after seed) so worldserver loads the item.
func SeedGuildCharter(db *sql.DB, ownerGUID uint64, guildName string) (itemLow uint32, petitionID uint32, err error) {
	// Ensure world will not treat the character as still online and overwrite inventory.
	_, _ = db.Exec(`UPDATE characters SET online=0 WHERE guid=?`, ownerGUID)

	var maxItem uint32
	if err = db.QueryRow(`SELECT IFNULL(MAX(guid),0) FROM item_instance`).Scan(&maxItem); err != nil {
		return 0, 0, err
	}
	itemLow = maxItem + 1

	petitionID = uint32(time.Now().Unix()%0x7fffffff) + 1
	for {
		var n int
		_ = db.QueryRow(`SELECT COUNT(*) FROM petition WHERE petition_id=?`, petitionID).Scan(&n)
		if n == 0 {
			break
		}
		petitionID++
	}

	// 12 enchantment slots * 3 fields = 36 ints; petition_id in first.
	ench := fmt.Sprintf("%d 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0", petitionID)

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()

	if _, err = tx.Exec(`DELETE FROM petition_sign WHERE ownerguid=?`, ownerGUID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec(`DELETE FROM petition WHERE ownerguid=? AND type=9`, ownerGUID); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec(`
		DELETE ci FROM character_inventory ci
		INNER JOIN item_instance ii ON ii.guid=ci.item
		WHERE ci.guid=? AND ii.itemEntry=?`, ownerGUID, ItemGuildCharter); err != nil {
		return 0, 0, err
	}
	if _, err = tx.Exec(`DELETE FROM item_instance WHERE owner_guid=? AND itemEntry=?`, ownerGUID, ItemGuildCharter); err != nil {
		return 0, 0, err
	}

	if _, err = tx.Exec(`
		INSERT INTO item_instance
		(guid, itemEntry, owner_guid, creatorGuid, giftCreatorGuid, count, duration, charges, flags, enchantments, randomPropertyId, durability, playedTime, text)
		VALUES (?, ?, ?, 0, 0, 1, 0, '0 0 0 0 0 ', 1, ?, 0, 0, 0, '')`,
		itemLow, ItemGuildCharter, ownerGUID, ench,
	); err != nil {
		return 0, 0, fmt.Errorf("item_instance: %w", err)
	}

	// Backpack slots in bag 0 are 23..38 (0..18 equipment, 19..22 bags).
	slot := -1
	for s := 23; s <= 38; s++ {
		var n int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM character_inventory WHERE guid=? AND bag=0 AND slot=?`, ownerGUID, s).Scan(&n)
		if n == 0 {
			slot = s
			break
		}
	}
	if slot < 0 {
		return 0, 0, fmt.Errorf("no free backpack slot")
	}
	if _, err = tx.Exec(`INSERT INTO character_inventory (guid, bag, slot, item) VALUES (?, 0, ?, ?)`, ownerGUID, slot, itemLow); err != nil {
		return 0, 0, fmt.Errorf("character_inventory: %w", err)
	}
	if _, err = tx.Exec(
		`INSERT INTO petition (ownerguid, petitionguid, petition_id, name, type) VALUES (?,?,?,?,9)`,
		ownerGUID, itemLow, petitionID, guildName,
	); err != nil {
		return 0, 0, fmt.Errorf("petition: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return itemLow, petitionID, nil
}

// SeedStackableItem places a tradable stackable item into the first free backpack slot.
// Character should be offline (or re-login after seed).
func SeedStackableItem(db *sql.DB, ownerGUID uint64, itemEntry, count uint32) (itemLow uint32, bag uint32, slot uint8, err error) {
	var maxItem uint32
	if err = db.QueryRow(`SELECT IFNULL(MAX(guid),0) FROM item_instance`).Scan(&maxItem); err != nil {
		return 0, 0, 0, err
	}
	itemLow = maxItem + 1

	tx, err := db.Begin()
	if err != nil {
		return 0, 0, 0, err
	}
	defer tx.Rollback()

	// Clear prior copies of this entry in backpack for a clean seed.
	if _, err = tx.Exec(`
		DELETE ci FROM character_inventory ci
		INNER JOIN item_instance ii ON ii.guid=ci.item
		WHERE ci.guid=? AND ii.itemEntry=?`, ownerGUID, itemEntry); err != nil {
		return 0, 0, 0, err
	}
	if _, err = tx.Exec(`DELETE FROM item_instance WHERE owner_guid=? AND itemEntry=?`, ownerGUID, itemEntry); err != nil {
		return 0, 0, 0, err
	}

	ench := "0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0"
	if _, err = tx.Exec(`
		INSERT INTO item_instance
		(guid, itemEntry, owner_guid, creatorGuid, giftCreatorGuid, count, duration, charges, flags, enchantments, randomPropertyId, durability, playedTime, text)
		VALUES (?, ?, ?, 0, 0, ?, 0, '0 0 0 0 0 ', 0, ?, 0, 0, 0, '')`,
		itemLow, itemEntry, ownerGUID, count, ench,
	); err != nil {
		return 0, 0, 0, fmt.Errorf("item_instance: %w", err)
	}

	slotFound := -1
	for s := 23; s <= 38; s++ {
		var n int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM character_inventory WHERE guid=? AND bag=0 AND slot=?`, ownerGUID, s).Scan(&n)
		if n == 0 {
			slotFound = s
			break
		}
	}
	if slotFound < 0 {
		return 0, 0, 0, fmt.Errorf("no free backpack slot")
	}
	if _, err = tx.Exec(`INSERT INTO character_inventory (guid, bag, slot, item) VALUES (?, 0, ?, ?)`, ownerGUID, slotFound, itemLow); err != nil {
		return 0, 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, 0, err
	}
	return itemLow, 0, uint8(slotFound), nil
}

// ItemGUID packs a HighGuid::Item ObjectGuid from the item_instance low guid.
func ItemGUID(low uint32) uint64 {
	return uint64(low) | (uint64(0x4000) << 48)
}

// CountGuildBankItems returns number of rows in guild_bank_item for guild/tab (tab < 0 = all).
func CountGuildBankItems(db *sql.DB, guildID uint32, tab int) (int, error) {
	var n int
	var err error
	if tab < 0 {
		err = db.QueryRow(`SELECT COUNT(*) FROM guild_bank_item WHERE guildid=?`, guildID).Scan(&n)
	} else {
		err = db.QueryRow(`SELECT COUNT(*) FROM guild_bank_item WHERE guildid=? AND TabId=?`, guildID, tab).Scan(&n)
	}
	return n, err
}

// GuildBankMoney returns guild.BankMoney for guildID.
func GuildBankMoney(db *sql.DB, guildID uint32) (uint64, error) {
	var m uint64
	err := db.QueryRow(`SELECT BankMoney FROM guild WHERE guildid=?`, guildID).Scan(&m)
	return m, err
}

// CountGuildBankTabs returns purchased tab count.
func CountGuildBankTabs(db *sql.DB, guildID uint32) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM guild_bank_tab WHERE guildid=?`, guildID).Scan(&n)
	return n, err
}
