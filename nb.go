package main

import (
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/russross/blackfriday.v2"
)

const ADMIN_ID = 1

const SUBMISSION = 0
const COMMENT = 1

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
	http.HandleFunc("/submit/", submitHandler(db))
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
		fmt.Fprintf(w, "  <li><a href=\"/submit/\">submit</a></li>\n")
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

		from := r.FormValue("from")

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

				fromurl := "/"
				if from != "" {
					fromurl, _ = url.QueryUnescape(from)
				}
				http.Redirect(w, r, fromurl, http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, login)

		fmt.Fprintf(w, "<section class=\"main\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/login/?from=%s\" method=\"post\">\n", from)
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
		s := `SELECT e.entry_id, e.title, e.url, e.createdt, 
IFNULL(u.user_id, 0), IFNULL(u.username, ''),  
(SELECT COUNT(*) FROM entry AS child WHERE child.parent_id = e.entry_id) AS ncomments 
FROM entry AS e 
LEFT OUTER JOIN user u ON e.user_id = u.user_id 
WHERE thing = 0 
ORDER BY createdt DESC`
		rows, err := db.Query(s)
		if err != nil {
			log.Fatal(err)
			return
		}

		fmt.Fprintf(w, "<ul class=\"vertical-list\">\n")
		var u User
		var e Entry
		var ncomments int
		for rows.Next() {
			rows.Scan(&e.Entryid, &e.Title, &e.Url, &e.Createdt, &u.Userid, &u.Username, &ncomments)
			screatedt := parseIsoDate(e.Createdt)
			itemurl := createItemUrl(e.Entryid)
			entryurl := e.Url
			if entryurl == "" {
				entryurl = itemurl
			}

			fmt.Fprintf(w, "<li>\n")

			fmt.Fprintf(w, "<div class=\"entry-title\">\n")
			fmt.Fprintf(w, "  <a href=\"%s\">%s</a>\n", entryurl, e.Title)
			if e.Url != "" {
				urllink, err := url.Parse(e.Url)
				urlhostname := strings.TrimPrefix(urllink.Hostname(), "www.")
				if err == nil {
					fmt.Fprintf(w, " <span class=\"text-grayed-2 text-sm\">(%s)</span>\n", urlhostname)
				}
			}
			fmt.Fprintf(w, "</div>\n")
			fmt.Fprintf(w, "<ul class=\"line-menu byline\">\n")
			fmt.Fprintf(w, "  <li><a href=\"#\">%s</a></li>\n", u.Username)
			fmt.Fprintf(w, "  <li>%s</li>\n", screatedt)
			fmt.Fprintf(w, "  <li><a href=\"%s\">Like</a></li>\n", "")
			fmt.Fprintf(w, "  <li><a href=\"%s\">%d %s</a></li>\n", itemurl, ncomments, getCountUnit(&e, ncomments))
			fmt.Fprintf(w, "</ul>\n")

			fmt.Fprintf(w, "</li>\n")
		}
		fmt.Fprintf(w, "</ul>\n")

		fmt.Fprintf(w, "</section>\n")
		printPageFoot(w)
	}
}

func getCountUnit(e *Entry, nchildren int) string {
	if e.Thing == SUBMISSION {
		if nchildren == 1 {
			return "comment"
		} else {
			return "comments"
		}
	} else {
		if nchildren == 1 {
			return "reply"
		} else {
			return "replies"
		}
	}
}

func validateIdParm(w http.ResponseWriter, id int64) bool {
	if id == -1 {
		http.Error(w, "Not found.", 404)
		return false
	}
	return true
}

func queryEntry(db *sql.DB, entryid int64) (*Entry, *User, *Entry, int, error) {
	var u User
	var e Entry
	var p Entry // parent
	var ncomments int

	s := `SELECT e.entry_id, e.thing, e.title, e.url, e.body, e.createdt, 
IFNULL(p.entry_id, 0), IFNULL(p.thing, 0), IFNULL(p.title, ''), IFNULL(p.url, ''), IFNULL(p.body, ''), IFNULL(p.createdt, ''), 
IFNULL(u.user_id, 0), IFNULL(u.username, ''), IFNULL(u.active, 0), IFNULL(u.email, ''),  
(SELECT COUNT(*) FROM entry AS child WHERE child.parent_id = e.entry_id) AS ncomments 
FROM entry e 
LEFT OUTER JOIN user u ON e.user_id = u.user_id 
LEFT OUTER JOIN entry p ON e.parent_id = p.entry_id 
WHERE e.entry_id = ?`
	row := db.QueryRow(s, entryid)
	err := row.Scan(&e.Entryid, &e.Thing, &e.Title, &e.Url, &e.Body, &e.Createdt,
		&p.Entryid, &p.Thing, &p.Title, &p.Url, &p.Body, &p.Createdt,
		&u.Userid, &u.Username, &u.Active, &u.Email, &ncomments)

	return &e, &u, &p, ncomments, err
}

func queryRootEntry(db *sql.DB, entryid int64) (*Entry, error) {
	nIteration := 0
	for {
		e, _, p, _, err := queryEntry(db, entryid)
		if err != nil {
			return nil, err
		}

		// If no parent, e is the root
		if p.Entryid == 0 {
			return e, nil
		}

		// Set a limit to number of iterations in case there's a circular reference
		nIteration++
		if nIteration > 100 {
			return e, nil
		}

		entryid = p.Entryid
	}
}

func handleDbErr(w http.ResponseWriter, err error, sfunc string) bool {
	if err == sql.ErrNoRows {
		http.Error(w, "Not found.", 404)
		return true
	}
	if err != nil {
		log.Printf("%s: database error (%s)\n", sfunc, err)
		http.Error(w, "Server database error.", 500)
		return true
	}
	return false
}

func validateLogin(w http.ResponseWriter, login *User) bool {
	if login.Userid == -1 {
		http.Error(w, "Not logged in.", 401)
		return false
	}
	if !login.Active {
		http.Error(w, "Not an active user.", 401)
		return false
	}
	return true
}

func createItemUrl(id int64) string {
	return fmt.Sprintf("/item/?id=%d", id)
}

func itemHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)

		entryid := idtoi(r.FormValue("id"))
		if !validateIdParm(w, entryid) {
			return
		}
		e, u, p, ncomments, err := queryEntry(db, entryid)
		if handleDbErr(w, err, "itemHandler") {
			return
		}

		itemurl := createItemUrl(e.Entryid)
		entryurl := e.Url
		if entryurl == "" {
			entryurl = itemurl
		}

		var errmsg string
		var comment Entry
		if r.Method == "POST" {
			if !validateLogin(w, login) {
				return
			}

			for {
				comment.Body = strings.TrimSpace(r.FormValue("commentbody"))
				if comment.Body == "" {
					errmsg = "Please enter a comment."
					break
				}

				comment.Body = strings.ReplaceAll(comment.Body, "\r", "") // CRLF => CR
				comment.Createdt = time.Now().Format(time.RFC3339)

				s := "INSERT INTO entry (thing, parent_id, title, body, createdt, user_id) VALUES (?, ?, ?, ?, ?, ?)"
				_, err := sqlexec(db, s, COMMENT, e.Entryid, "", comment.Body, comment.Createdt, login.Userid)
				if err != nil {
					log.Printf("DB error creating comment (%s)\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				http.Redirect(w, r, itemurl, http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, login)
		fmt.Fprintf(w, "<section class=\"main\">\n")

		if e.Thing == SUBMISSION {
			fmt.Fprintf(w, "<div class=\"entry-title\">\n")
			fmt.Fprintf(w, "  <a href=\"%s\">%s</a>\n", entryurl, e.Title)
			if e.Url != "" {
				urllink, err := url.Parse(e.Url)
				urlhostname := strings.TrimPrefix(urllink.Hostname(), "www.")
				if err == nil {
					fmt.Fprintf(w, " <span class=\"text-grayed-2 text-sm\">(%s)</span>\n", urlhostname)
				}
			}
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<ul class=\"line-menu byline\">\n")
		fmt.Fprintf(w, "  <li><a href=\"#\">%s</a></li>\n", u.Username)
		fmt.Fprintf(w, "  <li>%s</li>\n", parseIsoDate(e.Createdt))
		fmt.Fprintf(w, "  <li><a href=\"%s\">Like</a></li>\n", "")
		fmt.Fprintf(w, "  <li><a href=\"%s\">%d %s</a></li>\n", itemurl, ncomments, getCountUnit(e, ncomments))
		if e.Thing == COMMENT {
			parenturl := createItemUrl(p.Entryid)
			fmt.Fprintf(w, "  <li><a href=\"%s\">parent</a></li>\n", parenturl)

			root, err := queryRootEntry(db, e.Entryid)
			if err == nil {
				rooturl := createItemUrl(root.Entryid)
				fmt.Fprintf(w, "  <li>on: <a href=\"%s\">%s</a></li>\n", rooturl, root.Title)
			} else {
				log.Printf("DB error querying root entry (%s)\n", err)
			}
		}
		fmt.Fprintf(w, "</ul>\n")

		fmt.Fprintf(w, "<div class=\"entry-body content mt-base mb-base\">\n")
		fmt.Fprintf(w, parseMarkdown(e.Body))
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<form class=\"simpleform mb-2xl\" method=\"post\" action=\"%s\">\n", itemurl)
		if login.Userid == -1 || !login.Active {
			fmt.Fprintf(w, "<div class=\"control text-sm text-grayed-2 text-italic\">\n")
			fmt.Fprintf(w, "<label><a href=\"/login/?from=%s\">Log in</a> to post a comment.</label>\n", url.QueryEscape(r.RequestURI))
			fmt.Fprintf(w, "</div>\n")
		} else {
			if errmsg != "" {
				fmt.Fprintf(w, "<div class=\"control\">\n")
				fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
				fmt.Fprintf(w, "</div>\n")
			}
			fmt.Fprintf(w, "  <div class=\"control\">\n")
			fmt.Fprintf(w, "    <textarea id=\"commentbody\" name=\"commentbody\" rows=\"6\" cols=\"60\">%s</textarea>\n", comment.Body)
			fmt.Fprintf(w, "  </div>\n")
			fmt.Fprintf(w, "  <div class=\"control\">\n")
			fmt.Fprintf(w, "    <button class=\"submit\">add comment</button>\n")
			fmt.Fprintf(w, "  </div>\n")
		}
		fmt.Fprintf(w, "</form>\n")

		fmt.Fprintf(w, "<section class=\"entry-comments\">\n")
		printComments(w, db, e.Entryid, 0)
		fmt.Fprintf(w, "</section>\n")

		fmt.Fprintf(w, "</section>\n")
		printPageFoot(w)
	}
}

func printComments(w http.ResponseWriter, db *sql.DB, parentid int64, level int) {
	maxindent := 1

	s := `SELECT e.entry_id, e.body, e.createdt, u.user_id, u.username, uparent.user_id, uparent.username  
FROM entry AS e 
LEFT OUTER JOIN user u ON e.user_id = u.user_id 
LEFT OUTER JOIN entry p ON e.parent_id = p.entry_id 
LEFT OUTER JOIN user uparent ON uparent.user_id = p.user_id
WHERE e.thing = 1 AND e.parent_id = ? 
ORDER BY e.entry_id`
	rows, err := db.Query(s, parentid)
	if err != nil {
		log.Fatal(err)
		return
	}
	var reply Entry
	var u, uparent User
	for rows.Next() {
		rows.Scan(&reply.Entryid, &reply.Body, &reply.Createdt, &u.Userid, &u.Username, &uparent.Userid, &uparent.Username)
		sreplydt := parseIsoDate(reply.Createdt)
		replyurl := createItemUrl(reply.Entryid)

		// Limit the indents to maxindent
		nindent := level
		if nindent > maxindent {
			nindent = maxindent
		}
		fmt.Fprintf(w, "<div class=\"entry-comment\" style=\"padding-left: %drem\">\n", nindent*2)
		fmt.Fprintf(w, "  <p class=\"byline mb-xs\">%s <a href=\"%s\">%s</a></p>\n", u.Username, replyurl, sreplydt)
		fmt.Fprintf(w, "  <div class=\"entry-body content mt-xs mb-xs\">\n")
		if level >= 1 {
			fmt.Fprintf(w, "<span class=\"mention\">@%s</span> ", uparent.Username)
		}
		fmt.Fprintf(w, parseMarkdown(reply.Body))
		fmt.Fprintf(w, "  </div>\n")
		fmt.Fprintf(w, "  <p class=\"text-xs mb-base\"><a href=\"%s\">reply</a></p>\n", createItemUrl(reply.Entryid))
		fmt.Fprintf(w, "</div>\n")

		printComments(w, db, reply.Entryid, level+1)
	}
}

func submitHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)

		var e Entry

		var errmsg string
		if r.Method == "POST" {
			if !validateLogin(w, login) {
				return
			}

			for {
				e.Title = strings.TrimSpace(r.FormValue("title"))
				e.Url = strings.TrimSpace(r.FormValue("url"))
				e.Body = strings.TrimSpace(r.FormValue("body"))
				if e.Title == "" {
					errmsg = "Please enter a title."
					break
				}
				if e.Url == "" && e.Body == "" {
					errmsg = "Please enter a url or text writeup."
					break
				}

				e.Body = strings.ReplaceAll(e.Body, "\r", "") // CRLF => CR
				e.Createdt = time.Now().Format(time.RFC3339)

				s := "INSERT INTO entry (thing, title, url, body, createdt, user_id) VALUES (?, ?, ?, ?, ?, ?)"
				result, err := sqlexec(db, s, SUBMISSION, e.Title, e.Url, parseMarkdown(e.Body), e.Createdt, login.Userid)
				if err != nil {
					log.Printf("DB error creating submission (%s)\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}
				newid, err := result.LastInsertId()
				if err != nil {
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}
				http.Redirect(w, r, createItemUrl(newid), http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, login)
		fmt.Fprintf(w, "<section class=\"main\">\n")

		fmt.Fprintf(w, "<form class=\"simpleform mb-2xl\" method=\"post\" action=\"/submit/\">\n")
		if login.Userid == -1 || !login.Active {
			fmt.Fprintf(w, "<div class=\"control text-sm text-grayed-2 text-italic\">\n")
			fmt.Fprintf(w, "<label><a href=\"/login/?from=%s\">Log in</a> to post a comment.</label>\n", url.QueryEscape(r.RequestURI))
			fmt.Fprintf(w, "</div>\n")
		} else {
			if errmsg != "" {
				fmt.Fprintf(w, "<div class=\"control\">\n")
				fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
				fmt.Fprintf(w, "</div>\n")
			}

			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<label for=\"title\">title</label>\n")
			fmt.Fprintf(w, "<input id=\"title\" name=\"title\" type=\"text\" size=\"60\" value=\"%s\">\n", e.Title)
			fmt.Fprintf(w, "</div>\n")

			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<label for=\"url\">url</label>\n")
			fmt.Fprintf(w, "<input id=\"url\" name=\"url\" type=\"text\" size=\"60\" value=\"%s\">\n", e.Url)
			fmt.Fprintf(w, "</div>\n")

			fmt.Fprintf(w, "  <div class=\"control\">\n")
			fmt.Fprintf(w, "    <label for=\"body\">text</label>\n")
			fmt.Fprintf(w, "    <textarea id=\"body\" name=\"body\" rows=\"6\" cols=\"60\">%s</textarea>\n", e.Body)
			fmt.Fprintf(w, "  </div>\n")

			fmt.Fprintf(w, "  <div class=\"control\">\n")
			fmt.Fprintf(w, "    <button class=\"submit\">submit</button>\n")
			fmt.Fprintf(w, "  </div>\n")
		}
		fmt.Fprintf(w, "</form>\n")

		fmt.Fprintf(w, "</section>\n")
		printPageFoot(w)
	}
}
