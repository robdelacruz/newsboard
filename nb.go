package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	cryptorand "crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	sqlite "github.com/mattn/go-sqlite3"
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

type VoteResult struct {
	Entryid    int64 `json:"entryid"`
	Userid     int64 `json:"userid"`
	TotalVotes int   `json:"totalvotes"`
}

func main() {
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

	registerSqliteFuncs()
	db, err := sql.Open("sqlite3_custom", dbfile)
	if err != nil {
		fmt.Printf("Error opening '%s' (%s)\n", dbfile, err)
		os.Exit(1)
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	http.HandleFunc("/login/", loginHandler(db))
	http.HandleFunc("/logout/", logoutHandler(db))
	http.HandleFunc("/createaccount/", createaccountHandler(db))
	http.HandleFunc("/", indexHandler(db))
	http.HandleFunc("/item/", itemHandler(db))
	http.HandleFunc("/submit/", submitHandler(db))
	http.HandleFunc("/vote/", voteHandler(db))
	http.HandleFunc("/unvote/", unvoteHandler(db))
	port := "8000"
	fmt.Printf("Listening on %s...\n", port)
	err = http.ListenAndServe(fmt.Sprintf(":%s", port), nil)
	log.Fatal(err)
}

func registerSqliteFuncs() {
	rand.Seed(time.Now().UnixNano())

	sql.Register("sqlite3_custom", &sqlite.SQLiteDriver{
		ConnectHook: func(con *sqlite.SQLiteConn) error {
			err := con.RegisterFunc("pow", pow, true)
			if err != nil {
				return err
			}
			err = con.RegisterFunc("randint", randint, false)
			if err != nil {
				return err
			}
			err = con.RegisterFunc("seconds_since_epoch", seconds_since_epoch, false)
			if err != nil {
				return err
			}
			err = con.RegisterFunc("seconds_since_time", seconds_since_time, false)
			if err != nil {
				return err
			}
			err = con.RegisterFunc("hours_since_time", hours_since_time, false)
			if err != nil {
				return err
			}
			err = con.RegisterFunc("calculate_points", calculate_points, false)
			if err != nil {
				return err
			}
			return nil
		},
	})
}
func pow(n int, p float64) float64 {
	return math.Pow(float64(n), p)
}
func randint(n int) int {
	return rand.Intn(n)
}
func seconds_since_epoch(dt string) int64 {
	t, _ := time.Parse(time.RFC3339, dt)
	return t.Unix()
}
func seconds_since_time(dt string) int64 {
	return time.Now().Unix() - seconds_since_epoch(dt)
}
func hours_since_time(dt string) int64 {
	return seconds_since_time(dt) / 60 / 60
}
func calculate_points(votes int, submitdt string) float64 {
	return float64(votes) / pow((int(hours_since_time(submitdt))+2), 1.5)
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

func queryUsername(db *sql.DB, username string) *User {
	var u User
	u.Userid = -1

	s := "SELECT user_id, username, active, email FROM user WHERE username = ?"
	row := db.QueryRow(s, username)
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
		fmt.Fprintf(w, "<script src=\"%s\" defer></script>\n", jsurl)
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

func hashPassword(pwd string) string {
	hashedpwd, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(hashedpwd)
}

func loginUser(w http.ResponseWriter, userid int64) {
	suserid := fmt.Sprintf("%d", userid)
	c := http.Cookie{
		Name:  "userid",
		Value: suserid,
		Path:  "/",
		// Expires: time.Now().Add(24 * time.Hour),
	}
	http.SetCookie(w, &c)
}

func loginHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var username, password string

		login := getLoginUser(r, db)
		from := r.FormValue("from")

		if r.Method == "POST" {
			username = r.FormValue("username")
			password = r.FormValue("password")

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

				loginUser(w, userid)

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
		fmt.Fprintf(w, "<label for=\"username\">username</label>\n")
		fmt.Fprintf(w, "<input id=\"username\" name=\"username\" type=\"text\" size=\"20\" value=\"%s\">\n", username)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"password\">password</label>\n")
		fmt.Fprintf(w, "<input id=\"password\" name=\"password\" type=\"password\" size=\"20\" value=\"%s\">\n", password)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">login</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")

		fmt.Fprintf(w, "<p class=\"mt-xl\"><a href=\"/createaccount/?from=%s\">Create New Account</a></p>\n", from)
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

func isUsernameExists(db *sql.DB, username string) bool {
	s := "SELECT user_id FROM user WHERE username = ?"
	row := db.QueryRow(s, username)
	var userid int64
	err := row.Scan(&userid)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		return false
	}
	return true
}

func createaccountHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var username, email, password, password2 string

		login := getLoginUser(r, db)
		from := r.FormValue("from")

		if r.Method == "POST" {
			username = r.FormValue("username")
			email = r.FormValue("email")
			password = r.FormValue("password")
			password2 = r.FormValue("password2")
			for {
				if password != password2 {
					errmsg = "re-entered password doesn't match"
					password = ""
					password2 = ""
					break
				}
				if isUsernameExists(db, username) {
					errmsg = fmt.Sprintf("username '%s' already exists", username)
					break
				}

				hashedPassword := hashPassword(password)
				s := "INSERT INTO user (username, password, active, email) VALUES (?, ?, ?, ?);"
				result, err := sqlexec(db, s, username, hashedPassword, 1, email)
				if err != nil {
					log.Printf("DB error creating user: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}
				newid, err := result.LastInsertId()
				if err == nil {
					loginUser(w, newid)
				} else {
					// DB doesn't support getting newly added userid, so login manually.
					from = "/login/"
				}

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
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/createaccount/?from=%s\" method=\"post\">\n", from)
		fmt.Fprintf(w, "<h1 class=\"heading\">Create Account</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"username\">username</label>\n")
		fmt.Fprintf(w, "<input id=\"username\" name=\"username\" type=\"text\" size=\"20\" value=\"%s\">\n", username)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"email\">email</label>\n")
		fmt.Fprintf(w, "<input id=\"email\" name=\"email\" type=\"email\" size=\"20\" value=\"%s\">\n", email)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"password\">password</label>\n")
		fmt.Fprintf(w, "<input id=\"password\" name=\"password\" type=\"password\" size=\"20\" value=\"%s\">\n", password)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"password2\">re-enter password</label>\n")
		fmt.Fprintf(w, "<input id=\"password2\" name=\"password2\" type=\"password\" size=\"20\" value=\"%s\">\n", password2)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">create account</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
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

func getPointCountUnit(points int) string {
	if points == 1 {
		return "point"
	}
	return "points"
}

func printUpvote(w http.ResponseWriter, nvotes int, selfvote int) {
	var selfvoteclass string
	if selfvote == 1 {
		selfvoteclass = "selfvote "
	}
	fmt.Fprintf(w, "<a class=\"upvote %s mx-auto\">\n", selfvoteclass)
	fmt.Fprintf(w, "<svg viewbox=\"0 0 100 100\">\n")
	fmt.Fprintf(w, "  <polygon points=\"50 15, 100 100, 0 100\"/>\n")
	fmt.Fprintf(w, "</svg>\n")
	fmt.Fprintf(w, "</a>\n")
	fmt.Fprintf(w, "<div class=\"votectr mx-auto text-fade-2 text-sm\">%d</div>\n", nvotes)
}

func printCommentMarker(w http.ResponseWriter) {
	fmt.Fprintf(w, "<svg viewbox=\"0 0 100 100\" class=\"upvote\">\n")
	fmt.Fprintf(w, "  <rect x=\"25\" y=\"25\" width=\"40\" height=\"40\"/>\n")
	fmt.Fprintf(w, "</svg>\n")
}

func printSubmissionEntry(w http.ResponseWriter, db *sql.DB, e *Entry, submitter *User, login *User, ncomments, totalvotes int, selfvote int, points float64, showBody bool) {
	screatedt := parseIsoDate(e.Createdt)
	itemurl := createItemUrl(e.Entryid)
	entryurl := e.Url
	if entryurl == "" {
		entryurl = itemurl
	}

	tokparms := fmt.Sprintf("%d:%d", e.Entryid, login.Userid)
	fmt.Fprintf(w, "<section class=\"entry\" data-votetok=\"%s\">\n", encryptString(tokparms))
	fmt.Fprintf(w, "<div class=\"col0\">\n")
	printUpvote(w, totalvotes, selfvote)
	fmt.Fprintf(w, "</div>\n")

	fmt.Fprintf(w, "<div class=\"col1\">\n")
	fmt.Fprintf(w, "<div class=\"mb-xs text-lg\">\n")
	fmt.Fprintf(w, "  <a class=\"no-underline\" href=\"%s\">%s</a>\n", entryurl, e.Title)
	if e.Url != "" {
		urllink, err := url.Parse(e.Url)
		urlhostname := strings.TrimPrefix(urllink.Hostname(), "www.")
		if err == nil {
			fmt.Fprintf(w, " <span class=\"text-fade-2 text-sm\">(%s)</span>\n", urlhostname)
		}
	}
	fmt.Fprintf(w, "</div>\n")
	fmt.Fprintf(w, "<ul class=\"line-menu byline\">\n")
	npoints := int(math.Floor(points))
	fmt.Fprintf(w, "  <li>%d %s</li>\n", npoints, getPointCountUnit(npoints))
	fmt.Fprintf(w, "  <li><a href=\"#\">%s</a></li>\n", submitter.Username)
	fmt.Fprintf(w, "  <li>%s</li>\n", screatedt)
	fmt.Fprintf(w, "  <li><a href=\"%s\">%d %s</a></li>\n", itemurl, ncomments, getCountUnit(e, ncomments))
	fmt.Fprintf(w, "</ul>\n")

	if showBody {
		fmt.Fprintf(w, "<div class=\"content mt-base mb-base\">\n")
		fmt.Fprintf(w, parseMarkdown(e.Body))
		fmt.Fprintf(w, "</div>\n")
	}

	fmt.Fprintf(w, "</div>\n") // col1
	fmt.Fprintf(w, "</section>\n")
}

func printCommentEntry(w http.ResponseWriter, db *sql.DB, e *Entry, u *User, parent *Entry, ncomments int) {
	screatedt := parseIsoDate(e.Createdt)
	itemurl := createItemUrl(e.Entryid)
	entryurl := e.Url
	if entryurl == "" {
		entryurl = itemurl
	}

	fmt.Fprintf(w, "<section class=\"entry\">\n")
	fmt.Fprintf(w, "<div class=\"col0-comment\">\n")
	printCommentMarker(w)
	fmt.Fprintf(w, "</div>\n")

	fmt.Fprintf(w, "<div class=\"col1\">\n")
	fmt.Fprintf(w, "<ul class=\"line-menu byline\">\n")
	fmt.Fprintf(w, "  <li><a href=\"#\">%s</a></li>\n", u.Username)
	fmt.Fprintf(w, "  <li>%s</li>\n", screatedt)
	fmt.Fprintf(w, "  <li><a href=\"%s\">%d %s</a></li>\n", itemurl, ncomments, getCountUnit(e, ncomments))

	if parent != nil {
		parenturl := createItemUrl(parent.Entryid)
		fmt.Fprintf(w, "  <li><a href=\"%s\">parent</a></li>\n", parenturl)
	}

	root, err := queryRootEntry(db, e.Entryid)
	if err == nil {
		rooturl := createItemUrl(root.Entryid)
		fmt.Fprintf(w, "  <li>on: <a href=\"%s\">%s</a></li>\n", rooturl, root.Title)
	} else {
		log.Printf("DB error querying root entry (%s)\n", err)
	}
	fmt.Fprintf(w, "</ul>\n")

	fmt.Fprintf(w, "<div class=\"content mt-base mb-base\">\n")
	fmt.Fprintf(w, parseMarkdown(e.Body))
	fmt.Fprintf(w, "</div>\n")

	fmt.Fprintf(w, "</div>\n") // col1
	fmt.Fprintf(w, "</section>\n")
}

func printComment(w http.ResponseWriter, db *sql.DB, e *Entry, u *User, uparent *User, level int) {
	screatedt := parseIsoDate(e.Createdt)
	itemurl := createItemUrl(e.Entryid)
	entryurl := e.Url
	if entryurl == "" {
		entryurl = itemurl
	}

	// Limit the indents to maxindent
	maxindent := 1
	nindent := level
	if nindent > maxindent {
		nindent = maxindent
	}
	fmt.Fprintf(w, "<section class=\"entry\" style=\"padding-left: %drem\">\n", nindent*2)

	fmt.Fprintf(w, "<div class=\"col0-comment\">\n")
	printCommentMarker(w)
	fmt.Fprintf(w, "</div>\n")

	fmt.Fprintf(w, "<div class=\"col1\">\n")
	fmt.Fprintf(w, "  <p class=\"byline mb-xs\">%s <a href=\"%s\">%s</a></p>\n", u.Username, itemurl, screatedt)

	fmt.Fprintf(w, "  <div class=\"content mt-xs mb-xs\">\n")
	body := e.Body
	if level >= 1 {
		//		fmt.Fprintf(w, "<span class=\"mention\">@%s</span> ", uparent.Username)
		body = fmt.Sprintf("***@%s*** %s", uparent.Username, e.Body)
	}
	fmt.Fprintf(w, parseMarkdown(body))
	fmt.Fprintf(w, "</div>\n")

	fmt.Fprintf(w, "  <p class=\"text-xs mb-base\"><a href=\"%s\">reply</a></p>\n", createItemUrl(e.Entryid))
	fmt.Fprintf(w, "</div>\n") // col1

	fmt.Fprintf(w, "</section>\n")
}

func indexHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, []string{"/static/handlevote.js"}, nil)
		printPageNav(w, login)

		fmt.Fprintf(w, "<section class=\"main\">\n")
		s := `SELECT e.entry_id, e.title, e.url, e.createdt, 
IFNULL(u.user_id, 0), IFNULL(u.username, ''),  
(SELECT COUNT(*) FROM entry AS child WHERE child.parent_id = e.entry_id) AS ncomments, 
IFNULL(totalvotes.votes, 0),
CASE WHEN ev.entry_id IS NOT NULL THEN 1 ELSE 0 END, 
calculate_points(IFNULL(totalvotes.votes, 0), e.createdt) AS points
FROM entry AS e 
LEFT OUTER JOIN user u ON e.user_id = u.user_id 
LEFT OUTER JOIN totalvotes ON e.entry_id = totalvotes.entry_id 
LEFT OUTER JOIN entryvote ev ON ev.entry_id = e.entry_id AND ev.user_id = ? 
WHERE thing = 0 
ORDER BY points DESC`
		rows, err := db.Query(s, login.Userid)
		if err != nil {
			log.Fatal(err)
			return
		}

		fmt.Fprintf(w, "<ul class=\"vertical-list\">\n")
		var u User
		var e Entry
		var ncomments int
		var totalvotes int
		var selfvote int
		var points float64
		for rows.Next() {
			rows.Scan(&e.Entryid, &e.Title, &e.Url, &e.Createdt, &u.Userid, &u.Username, &ncomments, &totalvotes, &selfvote, &points)
			fmt.Fprintf(w, "<li>\n")
			printSubmissionEntry(w, db, &e, &u, login, ncomments, totalvotes, selfvote, points, false)
			fmt.Fprintf(w, "</li>\n")
		}
		fmt.Fprintf(w, "</ul>\n")

		fmt.Fprintf(w, "</section>\n")
		printPageFoot(w)
	}
}

func validateIdParm(w http.ResponseWriter, id int64) bool {
	if id == -1 {
		http.Error(w, "Not found.", 404)
		return false
	}
	return true
}

func queryEntry(db *sql.DB, entryid int64, login *User) (*Entry, *User, *Entry, int, int, int, float64, error) {
	var u User
	var e Entry
	var p Entry // parent
	var ncomments int
	var totalvotes int
	var selfvote int
	var points float64

	s := `SELECT e.entry_id, e.thing, e.title, e.url, e.body, e.createdt, 
IFNULL(p.entry_id, 0), IFNULL(p.thing, 0), IFNULL(p.title, ''), IFNULL(p.url, ''), IFNULL(p.body, ''), IFNULL(p.createdt, ''), 
IFNULL(u.user_id, 0), IFNULL(u.username, ''), IFNULL(u.active, 0), IFNULL(u.email, ''),  
(SELECT COUNT(*) FROM entry AS child WHERE child.parent_id = e.entry_id) AS ncomments, 
IFNULL(totalvotes.votes, 0) AS votes, 
CASE WHEN ev.entry_id IS NOT NULL THEN 1 ELSE 0 END, 
calculate_points(IFNULL(totalvotes.votes, 0), e.createdt) AS points
FROM entry e 
LEFT OUTER JOIN user u ON e.user_id = u.user_id 
LEFT OUTER JOIN totalvotes ON e.entry_id = totalvotes.entry_id 
LEFT OUTER JOIN entryvote ev ON ev.entry_id = e.entry_id AND ev.user_id = ?
LEFT OUTER JOIN entry p ON e.parent_id = p.entry_id 
WHERE e.entry_id = ?`
	row := db.QueryRow(s, login.Userid, entryid)
	err := row.Scan(&e.Entryid, &e.Thing, &e.Title, &e.Url, &e.Body, &e.Createdt,
		&p.Entryid, &p.Thing, &p.Title, &p.Url, &p.Body, &p.Createdt,
		&u.Userid, &u.Username, &u.Active, &u.Email, &ncomments, &totalvotes, &selfvote,
		&points)

	return &e, &u, &p, ncomments, totalvotes, selfvote, points, err
}

func queryRootEntry(db *sql.DB, entryid int64) (*Entry, error) {
	nIteration := 0
	for {
		var e, p Entry
		s := `SELECT e.entry_id, e.thing, e.title, e.url, e.body, e.createdt, 
IFNULL(p.entry_id, 0), IFNULL(p.thing, 0), IFNULL(p.title, ''), IFNULL(p.url, ''), IFNULL(p.body, ''), IFNULL(p.createdt, '') 
FROM entry e 
LEFT OUTER JOIN entry p ON e.parent_id = p.entry_id 
WHERE e.entry_id = ?`
		row := db.QueryRow(s, entryid)
		err := row.Scan(&e.Entryid, &e.Thing, &e.Title, &e.Url, &e.Body, &e.Createdt,
			&p.Entryid, &p.Thing, &p.Title, &p.Url, &p.Body, &p.Createdt)
		if err != nil {
			return nil, err
		}

		// If no parent, e is the root
		if p.Entryid == 0 {
			return &e, nil
		}

		// Set a limit to number of iterations in case there's a circular reference
		nIteration++
		if nIteration > 100 {
			return &e, nil
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
		e, u, p, ncomments, totalvotes, selfvote, points, err := queryEntry(db, entryid, login)
		if handleDbErr(w, err, "itemHandler") {
			return
		}
		itemurl := createItemUrl(e.Entryid)

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
		printPageHead(w, []string{"/static/handlevote.js"}, nil)
		printPageNav(w, login)

		fmt.Fprintf(w, "<section class=\"main\">\n")
		if e.Thing == SUBMISSION {
			printSubmissionEntry(w, db, e, u, login, ncomments, totalvotes, selfvote, points, true)
		} else if e.Thing == COMMENT {
			printCommentEntry(w, db, e, u, p, ncomments)
		}

		fmt.Fprintf(w, "<form class=\"simpleform mb-2xl\" method=\"post\" action=\"%s\">\n", itemurl)
		if login.Userid == -1 || !login.Active {
			fmt.Fprintf(w, "<div class=\"control text-sm text-fade-2 text-italic\">\n")
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

		printComment(w, db, &reply, &u, &uparent, level)
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
			fmt.Fprintf(w, "<div class=\"control text-sm text-fade-2 text-italic\">\n")
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

func decryptVoteTok(tok string) (int64, int64) {
	// When decrypted, tok takes the format: <entryid>:<userid>.
	// Ex. 123:12  where entryid = 123, userid = 12
	parms := decryptString(tok)
	sre := `^(\d+):(\d+)$`
	re := regexp.MustCompile(sre)
	matches := re.FindStringSubmatch(parms)
	if matches == nil {
		return -1, -1
	}

	entryid := idtoi(matches[1])
	userid := idtoi(matches[2])
	return entryid, userid
}

func voteHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		//todo authenticate request
		tok := r.FormValue("tok")
		if tok == "" {
			log.Printf("voteHandler: tok required\n")
			http.Error(w, "tok required", 401)
			return
		}

		entryid, userid := decryptVoteTok(tok)
		if entryid == -1 || userid == -1 {
			log.Printf("voteHandler: invalid tok format\n")
			http.Error(w, "invalid tok format", 401)
			return
		}

		e, err := queryEntryOnly(db, entryid)
		if err != nil {
			log.Printf("voteHandler: DB error (%s)\n", err)
			http.Error(w, "Server error", 500)
			return
		}
		if e == nil {
			log.Printf("voteHandler: entryid %d doesn't exist\n", entryid)
			http.Error(w, "entryid doesn't exist", 401)
			return
		}

		u := queryUser(db, userid)
		if u.Userid == -1 {
			log.Printf("voteHandler: userid %d doesn't exist\n", userid)
			http.Error(w, "user doesn't exist", 401)
			return
		}

		s := "INSERT OR REPLACE INTO entryvote (entry_id, user_id) VALUES (?, ?)"
		_, err = sqlexec(db, s, entryid, userid)
		if err != nil {
			log.Printf("voteHandler: DB error (%s)\n", err)
			http.Error(w, "Server error", 500)
			return
		}

		var votes int
		s = "SELECT IFNULL(totalvotes.votes, 0) AS votes FROM entry e LEFT OUTER JOIN totalvotes ON e.entry_id = totalvotes.entry_id WHERE e.entry_id = ?"
		row := db.QueryRow(s, entryid)
		err = row.Scan(&votes)
		if err != nil {
			log.Printf("voteHandler: DB error (%s)\n", err)
			votes = -1
		}

		vr := VoteResult{
			Entryid:    entryid,
			Userid:     userid,
			TotalVotes: votes,
		}
		bs, _ := json.Marshal(vr)
		w.WriteHeader(200)
		w.Write(bs)
	}
}

func unvoteHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		//todo authenticate request
		tok := r.FormValue("tok")
		if tok == "" {
			log.Printf("unvoteHandler: tok required\n")
			http.Error(w, "tok required", 401)
			return
		}

		entryid, userid := decryptVoteTok(tok)
		if entryid == -1 || userid == -1 {
			log.Printf("unvoteHandler: invalid tok format\n")
			http.Error(w, "invalid tok format", 401)
			return
		}

		e, err := queryEntryOnly(db, entryid)
		if err != nil {
			log.Printf("unvoteHandler: DB error (%s)\n", err)
			http.Error(w, "Server error", 500)
			return
		}
		if e == nil {
			log.Printf("unvoteHandler: entryid %d doesn't exist\n", entryid)
			http.Error(w, "entryid doesn't exist", 401)
			return
		}

		u := queryUser(db, userid)
		if u.Userid == -1 {
			log.Printf("unvoteHandler: userid %d doesn't exist\n", userid)
			http.Error(w, "user doesn't exist", 401)
			return
		}

		s := "DELETE FROM entryvote WHERE entry_id = ? AND user_id = ?"
		_, err = sqlexec(db, s, entryid, userid)
		if err != nil {
			log.Printf("unvoteHandler: DB error (%s)\n", err)
			http.Error(w, "Server error", 500)
			return
		}

		var votes int
		s = "SELECT IFNULL(totalvotes.votes, 0) AS votes FROM entry e LEFT OUTER JOIN totalvotes ON e.entry_id = totalvotes.entry_id WHERE e.entry_id = ?"
		row := db.QueryRow(s, entryid)
		err = row.Scan(&votes)
		if err != nil {
			log.Printf("unvoteHandler: DB error (%s)\n", err)
			votes = -1
		}

		vr := VoteResult{
			Entryid:    entryid,
			Userid:     userid,
			TotalVotes: votes,
		}
		bs, _ := json.Marshal(vr)
		w.WriteHeader(200)
		w.Write(bs)
	}
}

func queryEntryOnly(db *sql.DB, entryid int64) (*Entry, error) {
	s := "SELECT e.entry_id, e.thing, e.title, e.url, e.body, e.createdt FROM entry e WHERE e.entry_id = ?"
	row := db.QueryRow(s, entryid)
	var e Entry
	err := row.Scan(&e.Entryid, &e.Thing, &e.Title, &e.Url, &e.Body, &e.Createdt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// createHash(), encrypt() and decrypt() copied from Nic Raboy (thanks!) source article:
// https://www.thepolyglotdeveloper.com/2018/02/encrypt-decrypt-data-golang-application-crypto-packages/
func createHash(key string) string {
	hasher := md5.New()
	hasher.Write([]byte(key))
	return hex.EncodeToString(hasher.Sum(nil))
}

func encrypt(data []byte, passphrase string) []byte {
	block, _ := aes.NewCipher([]byte(createHash(passphrase)))
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err.Error())
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(cryptorand.Reader, nonce); err != nil {
		panic(err.Error())
	}
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext
}

func decrypt(data []byte, passphrase string) []byte {
	key := []byte(createHash(passphrase))
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err.Error())
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err.Error())
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return []byte{}
	}
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		panic(err.Error())
	}
	return plaintext
}

//todo Make this configurable and hidden.
const PASSPHRASE = "newsboard_passphrase"

func encryptString(s string) string {
	bs := encrypt([]byte(s), PASSPHRASE)
	return hex.EncodeToString(bs)
}

func decryptString(tok string) string {
	bstok, err := hex.DecodeString(tok)
	if err != nil {
		return ""
	}
	bs := decrypt(bstok, PASSPHRASE)
	return string(bs)
}
