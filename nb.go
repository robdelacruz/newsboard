package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/russross/blackfriday.v2"
)

const ADMIN_ID = 1

type User struct {
	Userid   int64
	Username string
	Active   bool
	Email    string
}

type Entry struct {
	Entryid  int64
	Thing    int
	Title    string
	Url      string
	Body     string
	Createdt string
	Userid   int64
	Parentid int64
}

func main() {
	port := "8000"

	os.Args = os.Args[1:]
	sw, parms := parseArgs(os.Args)

	// [-i new_file]  Create and initialize newsboard file
	if sw["i"] != "" {
		dbfile := sw["i"]
		if fileExists(dbfile) {
			s := fmt.Sprintf("File '%s' already exists. Can't initialize it.\n", dbfile)
			fmt.Printf(s)
			os.Exit(1)
		}
		createAndInitTables(dbfile)
		os.Exit(0)
	}

	// Need to specify a notes file as first parameter.
	if len(parms) == 0 {
		s := `Usage:

Start webservice using existing newsboard file:
	groupnotesd <newsboard_file>

Initialize new newsboard file:
	nb -i <newsboard_file>

`
		fmt.Printf(s)
		os.Exit(0)
	}

	// Exit if specified notes file doesn't exist.
	dbfile := parms[0]
	if !fileExists(dbfile) {
		s := fmt.Sprintf(`Newboard file '%s' doesn't exist. Create one using:
	nb -i <newsboard_file>
`, dbfile)
		fmt.Printf(s)
		os.Exit(1)
	}

	db, err := sql.Open("sqlite3", dbfile)
	if err != nil {
		fmt.Printf("Error opening '%s' (%s)\n", dbfile, err)
		os.Exit(1)
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	http.HandleFunc("/login/", loginHandler(db))
	http.HandleFunc("/logout/", logoutHandler(db))
	http.HandleFunc("/", indexHandler(db))
	http.HandleFunc("/item/", itemHandler(db))
	fmt.Printf("Listening on %s...\n", port)
	err = http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	log.Fatal(err)
}

func parseArgs(args []string) (map[string]string, []string) {
	switches := map[string]string{}
	parms := []string{}

	standaloneSwitches := []string{}
	definitionSwitches := []string{"i"}
	fNoMoreSwitches := false
	curKey := ""

	for _, arg := range args {
		if fNoMoreSwitches {
			// any arg after "--" is a standalone parameter
			parms = append(parms, arg)
		} else if arg == "--" {
			// "--" means no more switches to come
			fNoMoreSwitches = true
		} else if strings.HasPrefix(arg, "--") {
			switches[arg[2:]] = "y"
			curKey = ""
		} else if strings.HasPrefix(arg, "-") {
			if listContains(definitionSwitches, arg[1:]) {
				// -a "val"
				curKey = arg[1:]
				continue
			}
			for _, ch := range arg[1:] {
				// -a, -b, -ab
				sch := string(ch)
				if listContains(standaloneSwitches, sch) {
					switches[sch] = "y"
				}
			}
		} else if curKey != "" {
			switches[curKey] = arg
			curKey = ""
		} else {
			// standalone parameter
			parms = append(parms, arg)
		}
	}

	return switches, parms
}

func listContains(ss []string, v string) bool {
	for _, s := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func fileExists(file string) bool {
	_, err := os.Stat(file)
	if err != nil && os.IsNotExist(err) {
		return false
	}
	return true
}

func idtoi(sid string) int64 {
	if sid == "" {
		return -1
	}
	n, err := strconv.Atoi(sid)
	if err != nil {
		return -1
	}
	return int64(n)
}

func atoi(s string) int {
	if s == "" {
		return -1
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

func sqlstmt(db *sql.DB, s string) *sql.Stmt {
	stmt, err := db.Prepare(s)
	if err != nil {
		log.Fatalf("db.Prepare() sql: '%s'\nerror: '%s'", s, err)
	}
	return stmt
}

func sqlexec(db *sql.DB, s string, pp ...interface{}) (sql.Result, error) {
	stmt := sqlstmt(db, s)
	defer stmt.Close()
	return stmt.Exec(pp...)
}

func parseMarkdown(s string) string {
	return string(blackfriday.Run([]byte(s), blackfriday.WithExtensions(blackfriday.HardLineBreak)))
}

func parseIsoDate(dt string) string {
	tdt, _ := time.Parse(time.RFC3339, dt)
	return tdt.Format("2 Jan 2006")
}

func createAndInitTables(newfile string) {
	if fileExists(newfile) {
		s := fmt.Sprintf("File '%s' already exists. Can't initialize it.\n", newfile)
		fmt.Printf(s)
		os.Exit(1)
	}

	db, err := sql.Open("sqlite3", newfile)
	if err != nil {
		fmt.Printf("Error opening '%s' (%s)\n", newfile, err)
		os.Exit(1)
	}

	ss := []string{
		"BEGIN TRANSACTION;",
		"CREATE TABLE entry (entry_id INTEGER PRIMARY KEY NOT NULL, thing INTEGER NOT NULL, title TEXT, url TEXT, body TEXT, createdt TEXT, user_id INTEGER, parent_id INTEGER);",
		"CREATE TABLE user (user_id INTEGER PRIMARY KEY NOT NULL, username TEXT, password TEXT, active INTEGER NOT NULL, email TEXT, CONSTRAINT unique_username UNIQUE (username), CONSTRAINT unique_email UNIQUE (email));",
		"INSERT INTO user (user_id, username, password, active, email) VALUES (1, 'admin', '', 1, '');",
		"COMMIT;",
	}

	for _, s := range ss {
		_, err := sqlexec(db, s)
		if err != nil {
			log.Printf("DB error setting up newsboard db on '%s' (%s)\n", newfile, err)
			os.Exit(1)
		}
	}
}

func getLoginUser(r *http.Request, db *sql.DB) *User {
	var u User
	u.Userid = -1

	c, err := r.Cookie("userid")
	if err != nil {
		return &u
	}
	userid := idtoi(c.Value)
	if userid == -1 {
		return &u
	}
	return queryUser(db, userid)
}

func queryUser(db *sql.DB, userid int64) *User {
	var u User
	u.Userid = -1

	s := "SELECT user_id, username, active, email FROM user WHERE user_id = ?"
	row := db.QueryRow(s, userid)
	err := row.Scan(&u.Userid, &u.Username, &u.Active, &u.Email)
	if err == sql.ErrNoRows {
		return &u
	}
	if err != nil {
		fmt.Printf("queryUser() db error (%s)\n", err)
		return &u
	}
	return &u
}

func printPageHead(w io.Writer, jsurls []string, cssurls []string) {
	fmt.Fprintf(w, "<!DOCTYPE html>\n")
	fmt.Fprintf(w, "<html>\n")
	fmt.Fprintf(w, "<head>\n")
	fmt.Fprintf(w, "<meta charset=\"utf-8\">\n")
	fmt.Fprintf(w, "<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	fmt.Fprintf(w, "<title>Website</title>\n")
	fmt.Fprintf(w, "<link rel=\"stylesheet\" type=\"text/css\" href=\"/static/style.css\">\n")
	for _, cssurl := range cssurls {
		fmt.Fprintf(w, "<link rel=\"stylesheet\" type=\"text/css\" href=\"%s\">\n", cssurl)
	}
	for _, jsurl := range jsurls {
		fmt.Fprintf(w, "<script src=\"%s\"></script>\n", jsurl)
	}
	fmt.Fprintf(w, "</head>\n")
	fmt.Fprintf(w, "<body>\n")
	fmt.Fprintf(w, "<section class=\"body\">\n")
}

func printPageFoot(w io.Writer) {
	fmt.Fprintf(w, "</section>\n")
	fmt.Fprintf(w, "</body>\n")
	fmt.Fprintf(w, "</html>\n")
}

func printPageNav(w http.ResponseWriter, login *User) {
	fmt.Fprintf(w, "<header class=\"masthead mb-sm\">\n")
	fmt.Fprintf(w, "<nav class=\"navbar\">\n")

	// Menu section (left part)
	fmt.Fprintf(w, "<div>\n")
	title := "News Board"
	fmt.Fprintf(w, "<h1 class=\"heading\"><a href=\"/\">%s</a></h1>\n", title)
	fmt.Fprintf(w, "<ul class=\"line-menu\">\n")
	fmt.Fprintf(w, "  <li><a href=\"#\">latest</a></li>\n")
	if login.Userid != -1 && login.Active {
		fmt.Fprintf(w, "  <li><a href=\"#\">submit</a></li>\n")
	}
	fmt.Fprintf(w, "</ul>\n")
	fmt.Fprintf(w, "</div>\n")

	// User section (right part)
	fmt.Fprintf(w, "<ul class=\"line-menu right\">\n")
	if login.Userid != -1 {
		fmt.Fprintf(w, "<li><a href=\"#\">%s</a></li>\n", login.Username)
		fmt.Fprintf(w, "<li><a href=\"/logout\">logout</a></li>\n")
	} else {
		fmt.Fprintf(w, "<li><a href=\"/login\">login</a></li>\n")
	}
	fmt.Fprintf(w, "</ul>\n")

	fmt.Fprintf(w, "</nav>\n")
	fmt.Fprintf(w, "</header>\n")
}

func isCorrectPassword(inputPassword, hashedpwd string) bool {
	if hashedpwd == "" && inputPassword == "" {
		return true
	}
	err := bcrypt.CompareHashAndPassword([]byte(hashedpwd), []byte(inputPassword))
	if err != nil {
		return false
	}
	return true
}

func loginHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		login := getLoginUser(r, db)

		if r.Method == "POST" {
			username := r.FormValue("username")
			password := r.FormValue("password")

			s := "SELECT user_id, password, active FROM user WHERE username = ?"
			row := db.QueryRow(s, username, password)

			var userid int64
			var hashedpwd string
			var active int
			err := row.Scan(&userid, &hashedpwd, &active)

			for {
				if err == sql.ErrNoRows {
					errmsg = "Incorrect username or password"
					break
				}
				if err != nil {
					errmsg = "A problem occured. Please try again."
					break
				}
				if !isCorrectPassword(password, hashedpwd) {
					errmsg = "Incorrect username or password"
					break
				}
				if active == 0 {
					errmsg = fmt.Sprintf("User '%s' is inactive.", username)
					break
				}

				suserid := fmt.Sprintf("%d", userid)
				c := http.Cookie{
					Name:  "userid",
					Value: suserid,
					Path:  "/",
					// Expires: time.Now().Add(24 * time.Hour),
				}
				http.SetCookie(w, &c)

				// Display notes list page.
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, login)

		fmt.Fprintf(w, "<section class=\"main\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/login/\" method=\"post\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading\">Login</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label>username</label>\n")
		fmt.Fprintf(w, "<input name=\"username\" type=\"text\" size=\"20\">\n")
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label>password</label>\n")
		fmt.Fprintf(w, "<input name=\"password\" type=\"password\" size=\"20\">\n")
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">login</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageFoot(w)
	}
}

func logoutHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		c := http.Cookie{
			Name:   "userid",
			Value:  "",
			Path:   "/",
			MaxAge: 0,
		}
		http.SetCookie(w, &c)

		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func indexHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, login)

		fmt.Fprintf(w, "<section class=\"main\">\n")
		s := "SELECT entry_id, title, url, createdt, u.user_id, u.username FROM entry LEFT OUTER JOIN user u ON entry.user_id = u.user_id WHERE thing = 0 ORDER BY createdt DESC"
		rows, err := db.Query(s)
		if err != nil {
			log.Fatal(err)
			return
		}

		fmt.Fprintf(w, "<ul class=\"vertical-list\">\n")
		var u User
		var e Entry
		for rows.Next() {
			rows.Scan(&e.Entryid, &e.Title, &e.Url, &e.Createdt, &u.Userid, &u.Username)
			screatedt := parseIsoDate(e.Createdt)
			itemurl := fmt.Sprintf("/item/?id=%d", e.Entryid)
			entryurl := e.Url
			if entryurl == "" {
				entryurl = itemurl
			}

			fmt.Fprintf(w, "<li>\n")

			fmt.Fprintf(w, "<div class=\"entry-title\">\n")
			fmt.Fprintf(w, "  <a href=\"%s\">%s</a>\n", entryurl, e.Title)
			fmt.Fprintf(w, "</div>\n")
			fmt.Fprintf(w, "<ul class=\"line-menu byline\">\n")
			fmt.Fprintf(w, "  <li><a href=\"#\">%s</a></li>\n", u.Username)
			fmt.Fprintf(w, "  <li>%s</li>\n", screatedt)
			fmt.Fprintf(w, "  <li><a href=\"%s\">%d comments</a></li>\n", itemurl, 109)
			fmt.Fprintf(w, "</ul>\n")

			fmt.Fprintf(w, "</li>\n")
		}
		fmt.Fprintf(w, "</ul>\n")

		fmt.Fprintf(w, "</section>\n")
		printPageFoot(w)
	}
}

func itemHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)

		entryId := r.FormValue("id")
		if entryId == "" {
			http.Error(w, "Not found.", 404)
			return
		}

		var u User
		var e Entry

		s := "SELECT entry_id, title, url, body, createdt, u.user_id, u.username FROM entry LEFT OUTER JOIN user u ON entry.user_id = u.user_id WHERE thing = 0 AND entry.entry_id = ?"
		row := db.QueryRow(s, entryId)
		err := row.Scan(&e.Entryid, &e.Title, &e.Url, &e.Body, &e.Createdt, &u.Userid, &u.Username)
		if err == sql.ErrNoRows {
			http.Error(w, "Not found.", 404)
			return
		}
		screatedt := parseIsoDate(e.Createdt)
		itemurl := fmt.Sprintf("/item/?id=%d", e.Entryid)
		entryurl := e.Url
		if entryurl == "" {
			entryurl = itemurl
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, login)
		fmt.Fprintf(w, "<section class=\"main\">\n")

		fmt.Fprintf(w, "<div class=\"entry-title\">\n")
		fmt.Fprintf(w, "  <a href=\"%s\">%s</a>\n", entryurl, e.Title)
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "<ul class=\"line-menu byline\">\n")
		fmt.Fprintf(w, "  <li><a href=\"#\">%s</a></li>\n", u.Username)
		fmt.Fprintf(w, "  <li>%s</li>\n", screatedt)
		fmt.Fprintf(w, "  <li><a href=\"%s\">%d comments</a></li>\n", itemurl, 109)
		fmt.Fprintf(w, "</ul>\n")

		fmt.Fprintf(w, "<div class=\"entry-body content mt-base mb-base\">\n")
		fmt.Fprintf(w, parseMarkdown(e.Body))
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<form class=\"simpleform mb-2xl\">\n")
		fmt.Fprintf(w, "  <div class=\"control\">\n")
		fmt.Fprintf(w, "    <textarea id=\"replybody\" name=\"replybody\" rows=\"6\" cols=\"60\"></textarea>\n")
		fmt.Fprintf(w, "  </div>\n")
		fmt.Fprintf(w, "  <div class=\"control\">\n")
		fmt.Fprintf(w, "    <button class=\"submit\">add comment</button>\n")
		fmt.Fprintf(w, "  </div>\n")
		fmt.Fprintf(w, "</form>\n")

		fmt.Fprintf(w, "<section class=\"entry-comments\">\n")

		s = "SELECT entry_id, body, createdt, u.user_id, u.username FROM entry LEFT OUTER JOIN user u ON entry.user_id = u.user_id WHERE thing = 1 AND parent_id = ? ORDER BY entry_id"
		rows, err := db.Query(s, e.Entryid)
		if err != nil {
			log.Fatal(err)
			return
		}
		var reply Entry
		for rows.Next() {
			rows.Scan(&reply.Entryid, &reply.Body, &reply.Createdt, &u.Userid, &u.Username)
			sreplydt := parseIsoDate(reply.Createdt)
			replyurl := fmt.Sprintf("/item/?id=%d", reply.Entryid)

			fmt.Fprintf(w, "<p class=\"byline mb-xs\">%s <a href=\"%s\">%s</a></p>\n", u.Username, replyurl, sreplydt)
			fmt.Fprintf(w, "<div class=\"entry-body content mt-sm mb-sm\">\n")
			fmt.Fprintf(w, parseMarkdown(reply.Body))
			fmt.Fprintf(w, "</div>\n")
			fmt.Fprintf(w, "<p class=\"text-xs mb-base\"><a href=\"#\">reply</a></p>\n")
		}
		fmt.Fprintf(w, "</section>\n")

		fmt.Fprintf(w, "</section>\n")
		printPageFoot(w)
	}
}
