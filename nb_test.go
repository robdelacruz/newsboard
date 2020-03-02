package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"testing"
)

func TestSQL(t *testing.T) {
	// Exit if specified notes file doesn't exist.
	dbfile := "news.db"
	if !fileExists(dbfile) {
		s := fmt.Sprintf(`Newboard file '%s' doesn't exist. Create one using:
	nb -i <newsboard_file>
`, dbfile)
		fmt.Printf(s)
		os.Exit(1)
	}

	registerSqliteFuncs()
	db, err := sql.Open("sqlite3_custom", dbfile)
	if err != nil {
		fmt.Printf("Error opening '%s' (%s)\n", dbfile, err)
		os.Exit(1)
	}

	fmt.Printf("Test custom sqlite funcs.\n")

	s := "SELECT entry_id, pow(entry_id, 2.0), randint(10), seconds_since_epoch(createdt), seconds_since_time(createdt), hours_since_time(createdt), calculate_points(entry_id, createdt) FROM entry ORDER BY entry_id"
	rows, err := db.Query(s)
	if err != nil {
		log.Fatal(err)
		return
	}
	for rows.Next() {
		var npow int
		var randomnum int
		var entryid int64
		var secsEpoch, secsTime, hoursTime int64
		var points float64
		rows.Scan(&entryid, &npow, &randomnum, &secsEpoch, &secsTime, &hoursTime, &points)
		fmt.Printf("%d, %d, %d, %d, %d, %d, points=%f\n", entryid, npow, randomnum, secsEpoch, secsTime, hoursTime, points)
	}
}
