package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type HNItem struct {
	User  string
	Score int64
	Time  int64
	Title string
	Url   string
	Text  string
}

func handleErr(err error) {
	if err != nil {
		fmt.Printf("Error (%s)\n", err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) <= 1 {
		s := `Usage:

initdata <newsboard_file> [ask|top] [max_num_items_to_read]
`
		fmt.Printf(s)
		os.Exit(0)
	}

	dbfile := os.Args[1]
	maxitems := 20

	hnTopUrl := "https://hacker-news.firebaseio.com/v0/topstories.json?print=pretty"
	hnAskUrl := "https://hacker-news.firebaseio.com/v0/askstories.json?print=pretty"
	hnurl := hnTopUrl

	if len(os.Args) >= 3 {
		for _, arg := range os.Args[2:] {
			if arg == "ask" {
				hnurl = hnAskUrl
				continue
			}
			if arg == "top" {
				hnurl = hnTopUrl
				continue
			}

			fmt.Println(arg)
			n, err := strconv.Atoi(arg)
			if err == nil {
				maxitems = n
			}
		}
	}

	db, err := sql.Open("sqlite3", dbfile)
	if err != nil {
		fmt.Printf("Error opening '%s' (%s)\n", dbfile, err)
		os.Exit(1)
	}

	if hnurl == hnTopUrl {
		fmt.Printf("Reading 'Top Stories' from HackerNews...\n")
	} else if hnurl == hnAskUrl {
		fmt.Printf("Reading 'Ask HN' stories from HackerNews...\n")
	}
	resp, err := http.Get(hnurl)
	handleErr(err)

	bsTopStories, err := ioutil.ReadAll(resp.Body)
	handleErr(err)
	resp.Body.Close()

	fmt.Printf("Reading items into database...\n")
	var ids []int64
	err = json.Unmarshal(bsTopStories, &ids)
	handleErr(err)
	for i, id := range ids {
		if i >= maxitems {
			break
		}

		fmt.Printf("%d ", i+1)
		item := readItem(id)
		enterItem(db, item)
	}
	fmt.Printf("\n")
}

func parseIsoDate(dt string) string {
	tdt, _ := time.Parse(time.RFC3339, dt)
	return tdt.Format("2 Jan 2006")
}

func sqlstmt(db *sql.DB, s string) *sql.Stmt {
	stmt, err := db.Prepare(s)
	if err != nil {
		handleErr(err)
	}
	return stmt
}

func sqlexec(db *sql.DB, s string, pp ...interface{}) (sql.Result, error) {
	stmt := sqlstmt(db, s)
	defer stmt.Close()
	return stmt.Exec(pp...)
}

func readItem(id int64) HNItem {
	hnItemUrl := fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json?print=pretty", id)
	resp, err := http.Get(hnItemUrl)
	handleErr(err)

	bs, err := ioutil.ReadAll(resp.Body)
	handleErr(err)
	resp.Body.Close()

	var item HNItem
	err = json.Unmarshal(bs, &item)
	handleErr(err)
	return item
}

func enterItem(db *sql.DB, item HNItem) {
	s := "INSERT INTO entry (thing, user_id, title, url, body, createdt) VALUES (0, 1, ?, ?, ?, ?)"
	itemdt := time.Unix(item.Time, 0)
	screatedt := itemdt.Format("2006-01-02T15:04:05+07:00")
	_, err := sqlexec(db, s, item.Title, item.Url, item.Text, screatedt)
	handleErr(err)
}
