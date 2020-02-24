package main

import (
	"database/sql"
	"fmt"
	"html"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/russross/blackfriday.v2"
)

const ADMIN_ID = 1
const GUEST_ID = 2

const NOTE = 0
const REPLY = 1
const FILE = 2

const SIMPLEMDE_EDIT = 0
const TEXTAREA_EDIT = 1

const SETTINGS_LIMIT = 100

type User struct {
	Userid   int64
	Username string
	Active   bool
	Mdeditor int
}

type Site struct {
	Title                   string
	Desc                    string
	RequireLoginForPageview bool
	AllowAnonReplies        bool
	Loginmsg                string
	Sidebar1                string
	Sidebar2                string
}

func main() {
	port := "8000"

	os.Args = os.Args[1:]
	sw, parms := parseArgs(os.Args)

	// [-i new_file]  Create and initialize new notes file
	if sw["i"] != "" {
		newnotesfile := sw["i"]
		if fileExists(newnotesfile) {
			s := fmt.Sprintf("File '%s' already exists. Can't initialize it.\n", newnotesfile)
			fmt.Printf(s)
			os.Exit(1)
		}
		createAndInitTables(newnotesfile)
		os.Exit(0)
	}

	// Need to specify a notes file as first parameter.
	if len(parms) == 0 {
		s := `Usage:

Start webservice using existing notes file:
	groupnotesd <notes_file>

Initialize new notes file:
	groupnotesd -i <notes_file>

`
		fmt.Printf(s)
		os.Exit(0)
	}

	// Exit if specified notes file doesn't exist.
	notesfile := parms[0]
	if !fileExists(notesfile) {
		s := fmt.Sprintf(`Notes file '%s' doesn't exist. Create one using:
	groupnotesd -i <notes_file>
`, notesfile)
		fmt.Printf(s)
		os.Exit(1)
	}

	db, err := sql.Open("sqlite3", notesfile)
	if err != nil {
		fmt.Printf("Error opening '%s' (%s)\n", notesfile, err)
		os.Exit(1)
	}

	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./static"))))
	http.HandleFunc("/", notesHandler(db))
	http.HandleFunc("/note/", noteHandler(db))
	http.HandleFunc("/createnote/", createNoteHandler(db))
	http.HandleFunc("/editnote/", editNoteHandler(db))
	http.HandleFunc("/delnote/", delNoteHandler(db))
	http.HandleFunc("/browsefiles/", browsefilesHandler(db))
	http.HandleFunc("/search/", searchHandler(db))
	http.HandleFunc("/file/", fileHandler(db))
	http.HandleFunc("/uploadfile/", uploadFileHandler(db))
	http.HandleFunc("/editfile/", editFileHandler(db))
	http.HandleFunc("/delfile/", delFileHandler(db))
	http.HandleFunc("/newreply/", newReplyHandler(db))
	http.HandleFunc("/editreply/", editReplyHandler(db))
	http.HandleFunc("/delreply/", delReplyHandler(db))
	http.HandleFunc("/login/", loginHandler(db))
	http.HandleFunc("/logout/", logoutHandler(db))
	http.HandleFunc("/adminsetup/", adminsetupHandler(db))
	http.HandleFunc("/usersettings/", usersettingsHandler(db))
	http.HandleFunc("/newuser/", newUserHandler(db))
	http.HandleFunc("/edituser/", editUserHandler(db))
	http.HandleFunc("/activateuser/", activateUserHandler(db))
	http.HandleFunc("/sitesettings/", sitesettingsHandler(db))
	http.HandleFunc("/userssetup/", userssetupHandler(db))
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
		"DROP TABLE IF EXISTS notereply;",
		"DROP TABLE IF EXISTS note;",
		"DROP TABLE IF EXISTS file;",
		"CREATE TABLE note (note_id INTEGER PRIMARY KEY NOT NULL, title TEXT, body TEXT, createdt TEXT, user_id INTEGER, FOREIGN KEY(user_id) REFERENCES user);",
		"CREATE TABLE notereply (notereply_id INTEGER PRIMARY KEY NOT NULL, note_id INTEGER, replybody TEXT, createdt TEXT, user_id INTEGER, FOREIGN KEY(user_id) REFERENCES user, FOREIGN KEY(note_id) REFERENCES note);",
		"CREATE TABLE file (file_id INTEGER PRIMARY KEY NOT NULL, filename TEXT, folder TEXT, desc TEXT, content BLOB, createdt TEXT, user_id INTEGER, FOREIGN KEY(user_id) REFERENCES user);",
		"DROP TABLE IF EXISTS user;",
		"CREATE TABLE user (user_id INTEGER PRIMARY KEY NOT NULL, username TEXT, password TEXT, CONSTRAINT unique_username UNIQUE (username));",
		"DROP TABLE IF EXISTS site;",
		"CREATE TABLE site (site_id INTEGER PRIMARY KEY NOT NULL, title TEXT, desc TEXT);",
		"INSERT INTO user (user_id, username, password) VALUES (1, 'admin', '');",
		"COMMIT;",
	}

	for _, s := range ss {
		_, err := sqlexec(db, s)
		if err != nil {
			log.Printf("DB error setting up notes db on '%s' (%s)\n", newfile, err)
			os.Exit(1)
		}
	}
}

func parseNoteid(url string) int64 {
	sre := `^/note/(\d+)$`
	re := regexp.MustCompile(sre)
	matches := re.FindStringSubmatch(url)
	if matches == nil {
		return -1
	}
	return idtoi(matches[1])
}

func notesHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)
		if !hasPageAccess(login, querySite(db)) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		}

		w.Header().Set("Content-Type", "text/html")

		offset := atoi(r.FormValue("offset"))
		if offset <= 0 {
			offset = 0
		}
		limit := atoi(r.FormValue("limit"))
		if limit <= 0 {
			limit = SETTINGS_LIMIT
		}

		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<ul class=\"vertical-list compact\">\n")
		s := `SELECT entry_id, title, summary, createdt, user.user_id, username, 
(SELECT COUNT(*) FROM entry AS reply WHERE reply.thing = 1 AND reply.parent_id = note.entry_id) AS numreplies, 
(SELECT COALESCE(MAX(reply.createdt), '') FROM entry AS reply where reply.thing = 1 AND reply.parent_id = note.entry_id) AS maxreplydt 
FROM entry AS note 
LEFT OUTER JOIN user on note.user_id = user.user_id 
WHERE note.thing = 0 
ORDER BY MAX(createdt, maxreplydt) DESC 
LIMIT ? OFFSET ?`
		rows, err := db.Query(s, limit, offset)
		if err != nil {
			log.Fatal(err)
			return
		}
		nrows := 0
		for rows.Next() {
			var noteid int64
			var title, summary, createdt, maxreplydt string
			var noteUser User
			var numreplies int
			rows.Scan(&noteid, &title, &summary, &createdt, &noteUser.Userid, &noteUser.Username, &numreplies, &maxreplydt)
			tcreatedt, _ := time.Parse(time.RFC3339, createdt)

			fmt.Fprintf(w, "<li>\n")
			fmt.Fprintf(w, "<h2 class=\"doc-title heading\"><a href=\"/note/%d\">%s</a></h2>\n", noteid, title)
			if summary != "" {
				fmt.Fprintf(w, "<div class=\"smalltext italic\">\n")
				fmt.Fprintf(w, parseMarkdown(summary))
				fmt.Fprintf(w, "</div>\n")
			}
			printByline(w, login, noteid, &noteUser, tcreatedt, numreplies)
			fmt.Fprintf(w, "</li>\n")

			nrows++
		}
		fmt.Fprintf(w, "</ul>\n")
		printPagingNav(w, "/?", offset, limit, nrows)
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func noteHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		noteid := parseNoteid(r.URL.Path)
		if noteid == -1 {
			log.Printf("display note: no noteid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		login := getLoginUser(r, db)
		if !hasPageAccess(login, querySite(db)) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		}

		s := `SELECT title, summary, body, createdt, user.user_id, username, (SELECT COUNT(*) FROM entry as reply WHERE reply.thing = 1 AND reply.parent_id = note.entry_id) AS numreplies 
FROM entry AS note 
LEFT OUTER JOIN user ON note.user_id = user.user_id
WHERE note.entry_id = ? AND note.thing = 0 
ORDER BY createdt DESC;`
		row := db.QueryRow(s, noteid)

		var title, summary, body, createdt string
		var noteUser User
		var numreplies int
		err := row.Scan(&title, &summary, &body, &createdt, &noteUser.Userid, &noteUser.Username, &numreplies)
		if err == sql.ErrNoRows {
			// note doesn't exist so redirect to notes list page.
			log.Printf("display note: noteid %d doesn't exist\n", noteid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		if login.Mdeditor == SIMPLEMDE_EDIT {
			printPageHead(w, []string{
				"/static/simplemde.min.js",
			}, []string{
				"/static/simplemde.min.css",
			})
		} else {
			printPageHead(w, nil, nil)
		}
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<article class=\"content\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading doc-title\"><a href=\"/note/%d\">%s</a></h1>\n", noteid, title)
		tcreatedt, err := time.Parse(time.RFC3339, createdt)
		if err != nil {
			tcreatedt = time.Now()
		}
		if summary != "" {
			fmt.Fprintf(w, "<div class=\"smalltext italic compact\">\n")
			fmt.Fprintf(w, parseMarkdown(summary))
			fmt.Fprintf(w, "</div>\n")
		}
		printByline(w, login, noteid, &noteUser, tcreatedt, numreplies)

		bodyMarkup := parseMarkdown(body)
		fmt.Fprintf(w, bodyMarkup)

		fmt.Fprintf(w, "<hr class=\"dotted\">\n")
		fmt.Fprintf(w, "<p>Replies:</p>\n")

		s = "SELECT entry_id, body, createdt, user.user_id, username FROM entry AS reply LEFT OUTER JOIN user ON reply.user_id = user.user_id WHERE reply.parent_id = ? AND reply.thing = 1 ORDER BY reply.entry_id"
		rows, err := db.Query(s, noteid)
		if err != nil {
			fmt.Fprintf(w, "<p class=\"error\">Error loading replies</p>\n")
			fmt.Fprintf(w, "</article>\n")
			printPageFoot(w)
			return
		}
		i := 1
		for rows.Next() {
			var replyid int64
			var replybody, createdt string
			var replyUser User
			rows.Scan(&replyid, &replybody, &createdt, &replyUser.Userid, &replyUser.Username)
			tcreatedt, _ := time.Parse(time.RFC3339, createdt)
			createdt = tcreatedt.Format("2 Jan 2006")

			fmt.Fprintf(w, "<div class=\"reply compact\">\n")
			fmt.Fprintf(w, "<ul class=\"line-menu finetext\">\n")
			fmt.Fprintf(w, "<li><a id=\"reply-%d\">%d.</a> %s</li>\n", replyid, i, replyUser.Username)
			fmt.Fprintf(w, "<li>%s</li>\n", createdt)
			// Show edit/delete links if admin or active user owner of note.
			if login.Userid == ADMIN_ID || (replyUser.Userid == login.Userid && login.Active) {
				fmt.Fprintf(w, "<li><a href=\"/editreply/?replyid=%d\">Edit</a></li>\n", replyid)
				fmt.Fprintf(w, "<li><a href=\"/delreply/?replyid=%d\">Delete</a></li>\n", replyid)
			}
			fmt.Fprintf(w, "</ul>\n")
			fmt.Fprintf(w, "<div id=\"replybody-%d\">\n", i)
			replybodyMarkup := parseMarkdown(replybody)
			fmt.Fprintf(w, replybodyMarkup)
			fmt.Fprintf(w, "</div>\n")
			fmt.Fprintf(w, "</div>\n")

			i++
		}
		fmt.Fprintf(w, "</article>\n")

		// New Reply form
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/newreply/?noteid=%d\" method=\"post\">\n", noteid)
		// If not logged in and 'allow anonymous replies' is on, use Guest user to reply.
		site := querySite(db)
		if login.Userid == -1 && site.AllowAnonReplies {
			login = queryUser(db, GUEST_ID)
		}
		if login.Userid == -1 || !login.Active {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<label><a href=\"/login/\">Log in</a> to post a reply.</label>")
			fmt.Fprintf(w, "</div>\n")
		} else {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<a id=\"newreply\"></a>\n")
			fmt.Fprintf(w, "<label for=\"replybody\">reply as %s:</label>\n", login.Username)
			fmt.Fprintf(w, "<textarea id=\"replybody\" name=\"replybody\" rows=\"10\"></textarea>\n")
			fmt.Fprintf(w, "</div>\n")

			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<button class=\"submit\">add reply</button>\n")
			fmt.Fprintf(w, "</div>\n")
			fmt.Fprintf(w, "</form>\n")
		}

		// previous and next note nav
		prevNoteid, prevNoteTitle := queryPrevNoteid(db, noteid)
		nextNoteid, nextNoteTitle := queryNextNoteid(db, noteid)
		if prevNoteid != -1 || nextNoteid != -1 {
			fmt.Fprintf(w, "<nav class=\"pagenav smalltext doc-title2 margin-top margin-bottom\">\n")
			fmt.Fprintf(w, "<div>\n")
			if prevNoteid != -1 {
				fmt.Fprintf(w, fmt.Sprintf("<a href=\"/note/%d\">&lt;&lt; %s</a>\n", prevNoteid, prevNoteTitle))
			}
			fmt.Fprintf(w, "</div>\n")
			fmt.Fprintf(w, "<div>\n")
			if nextNoteid != -1 {
				fmt.Fprintf(w, fmt.Sprintf("<a href=\"/note/%d\">&gt;&gt; %s</a>\n", nextNoteid, nextNoteTitle))
			}
			fmt.Fprintf(w, "</div>\n")
			fmt.Fprintf(w, "</nav>\n")
		}
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")

		if login.Mdeditor == SIMPLEMDE_EDIT {
			printSimpleMDECode(w, "replybody")
		}
		printPageFoot(w)
	}
}

func createNoteHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var title, summary, body string

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("create note: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			title = r.FormValue("title")
			summary = r.FormValue("summary")
			body = r.FormValue("body")
			body = strings.ReplaceAll(body, "\r", "") // CRLF => CR
			createdt := time.Now().Format(time.RFC3339)

			s := "INSERT INTO entry (thing, title, summary, body, createdt, user_id) VALUES (?, ?, ?, ?, ?, ?);"
			_, err := sqlexec(db, s, NOTE, title, summary, body, createdt, login.Userid)
			if err != nil {
				log.Printf("DB error creating note: %s\n", err)
				errmsg = "A problem occured. Please try again."
			}

			if errmsg == "" {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		if login.Mdeditor == SIMPLEMDE_EDIT {
			printPageHead(w, []string{
				"/static/simplemde.min.js",
			}, []string{
				"/static/simplemde.min.css",
			})
		} else {
			printPageHead(w, nil, nil)
		}
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/createnote/\" method=\"post\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading\">Create Note</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label>title</label>\n")
		fmt.Fprintf(w, "<input name=\"title\" type=\"text\" size=\"50\" value=\"%s\">\n", title)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"summary\">summary</label>\n")
		fmt.Fprintf(w, "<textarea id=\"summary\" name=\"summary\" rows=\"3\">%s</textarea>\n", summary)

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"body\">body</label>\n")
		fmt.Fprintf(w, "<textarea id=\"body\" name=\"body\" rows=\"25\">%s</textarea>\n", body)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">create note</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")

		if login.Mdeditor == SIMPLEMDE_EDIT {
			printSimpleMDECode(w, "body")
		}
		printPageFoot(w)
	}
}

func printSimpleMDECode(w http.ResponseWriter, textid string) {
	fmt.Fprintf(w, "<script>\n")
	fmt.Fprintf(w, "let mde = new SimpleMDE({element: document.querySelector(\"#%s\"), indentWithTabs: false});\n", textid)
	fmt.Fprintf(w, "</script>\n")
}

func editNoteHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string

		noteid := idtoi(r.FormValue("noteid"))
		if noteid == -1 {
			log.Printf("edit note: no noteid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("edit note: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		s := "SELECT title, summary, body, user_id FROM entry WHERE entry_id = ? AND thing = 0"
		row := db.QueryRow(s, noteid)

		var title, summary, body string
		var noteUserid int64
		err := row.Scan(&title, &summary, &body, &noteUserid)
		if err == sql.ErrNoRows {
			// note doesn't exist so redirect to notes list page.
			log.Printf("noteid %d doesn't exist\n", noteid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		// Allow only creators or admin to edit the note.
		if noteUserid != login.Userid && login.Userid != ADMIN_ID {
			fmt.Printf("login userid: %d, note userid: %d\n", login.Userid, noteUserid)
			log.Printf("User '%s' doesn't have access to note %d\n", login.Username, noteid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			title = r.FormValue("title")
			summary = r.FormValue("summary")
			body = r.FormValue("body")
			createdt := time.Now().Format(time.RFC3339)

			// Strip out linefeed chars so that CRLF becomes just CR.
			// CRLF causes problems in markdown parsing.
			body = strings.ReplaceAll(body, "\r", "")

			s := "UPDATE entry SET title = ?, summary = ?, body = ?, createdt = ? WHERE entry_id = ? AND thing = 0"
			_, err = sqlexec(db, s, title, summary, body, createdt, noteid)
			if err != nil {
				log.Printf("DB error updating noteid %d: %s\n", noteid, err)
				errmsg = "A problem occured. Please try again."
			}

			if errmsg == "" {
				http.Redirect(w, r, fmt.Sprintf("/note/%d", noteid), http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		if login.Mdeditor == SIMPLEMDE_EDIT {
			printPageHead(w, []string{
				"/static/simplemde.min.js",
			}, []string{
				"/static/simplemde.min.css",
			})
		} else {
			printPageHead(w, nil, nil)
		}
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/editnote/?noteid=%d\" method=\"post\">\n", noteid)
		fmt.Fprintf(w, "<h1 class=\"heading\">Edit Note</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label>title</label>\n")
		fmt.Fprintf(w, "<input name=\"title\" type=\"text\" size=\"50\" value=\"%s\">\n", title)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"summary\">summary</label>\n")
		fmt.Fprintf(w, "<textarea id=\"summary\" name=\"summary\" rows=\"3\">%s</textarea>\n", summary)

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"body\">note</label>\n")
		fmt.Fprintf(w, "<textarea id=\"body\" name=\"body\" rows=\"25\">%s</textarea>\n", body)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">update note</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")

		if login.Mdeditor == SIMPLEMDE_EDIT {
			printSimpleMDECode(w, "body")
		}
		printPageFoot(w)
	}
}

func delNoteHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string

		noteid := idtoi(r.FormValue("noteid"))
		if noteid == -1 {
			log.Printf("del note: no noteid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("del note: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		s := "SELECT title, summary, body, user_id FROM entry WHERE entry_id = ? AND thing = 0"
		row := db.QueryRow(s, noteid)

		var title, summary, body string
		var noteUserid int64
		err := row.Scan(&title, &summary, &body, &noteUserid)
		if err == sql.ErrNoRows {
			// note doesn't exist so redirect to notes list page.
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		// Allow only creators or admin to delete the note.
		if noteUserid != login.Userid && login.Userid != ADMIN_ID {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			// todo: use transaction or trigger instead?

			for {
				// Delete note replies
				s = "DELETE FROM entry AS reply WHERE reply.parent_id = ? AND thing = 1"
				_, err = sqlexec(db, s, noteid)
				if err != nil {
					log.Printf("DB error deleting notereplies of noteid %d: %s\n", noteid, err)
					errmsg = "A problem occured. Please try again."
					break
				}

				// Delete note
				s := "DELETE FROM entry WHERE entry_id = ? AND thing = 0"
				_, err = sqlexec(db, s, noteid)
				if err != nil {
					log.Printf("DB error deleting noteid %d: %s\n", noteid, err)
					errmsg = "A problem occured. Please try again."
					break
				}

				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform displayonly\" action=\"/delnote/?noteid=%d\" method=\"post\">\n", noteid)
		fmt.Fprintf(w, "<h1 class=\"heading warning\">Delete Note</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label class=\"byline\">title</label>\n")
		fmt.Fprintf(w, "<input name=\"title\" type=\"text\" size=\"50\" readonly value=\"%s\">\n", title)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"summary\">summary</label>\n")
		fmt.Fprintf(w, "<textarea id=\"summary\" name=\"summary\" rows=\"3\">%s</textarea>\n", summary)

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label class=\"byline\">note</label>\n")
		fmt.Fprintf(w, "<textarea name=\"body\" rows=\"25\" readonly>%s</textarea>\n", body)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit warning\">delete note</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func parseFileid(url string) int64 {
	sre := `^/file/(\d+)$`
	re := regexp.MustCompile(sre)
	matches := re.FindStringSubmatch(url)
	if matches == nil {
		return -1
	}
	return idtoi(matches[1])
}

func parseUrlFilepath(url string) (string, string) {
	sre := `^/file/(.+)$`
	re := regexp.MustCompile(sre)
	matches := re.FindStringSubmatch(url)
	if matches == nil {
		return "", ""
	}

	ss := strings.Split(matches[1], "/")
	if len(ss) == 1 {
		return "", ss[0]
	}
	path := strings.Join(ss[:len(ss)-1], "/")
	if len(path) == 0 {
		path = ""
	}
	name := ss[len(ss)-1]
	return path, name
}

func isFileExtImg(ext string) bool {
	if ext == "png" || ext == "gif" || ext == "bmp" || ext == "jpg" || ext == "jpeg" {
		return true
	}
	return false
}

func browsefilesHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)
		if !hasPageAccess(login, querySite(db)) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		}

		w.Header().Set("Content-Type", "text/html")

		offset := atoi(r.FormValue("offset"))
		if offset <= 0 {
			offset = 0
		}
		limit := atoi(r.FormValue("limit"))
		if limit <= 0 {
			limit = SETTINGS_LIMIT
		}
		outputfmt := r.FormValue("outputfmt")
		if outputfmt == "" {
			outputfmt = "table"
		}

		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmtlink := "Grid"
		fmtparam := "grid"
		if outputfmt == "grid" {
			fmtlink = "Table"
			fmtparam = "table"
		}
		fmt.Fprintf(w, "<div class=\"flex-row\">\n")
		fmt.Fprintf(w, "  <h1 class=\"heading\">Browse Files</h1>\n")
		fmt.Fprintf(w, "  <a class=\"smalltext\" href=\"/browsefiles?offset=%d&limit=%d&outputfmt=%s\">View as %s</a>\n", offset, limit, fmtparam, fmtlink)
		fmt.Fprintf(w, "</div>\n")

		s := "SELECT entry.entry_id, title, folder, body, createdt, user.user_id, username FROM entry INNER JOIN file ON entry.entry_id = file.entry_id LEFT OUTER JOIN user ON entry.user_id = user.user_id WHERE thing = 2 ORDER BY createdt DESC LIMIT ? OFFSET ?"
		rows, err := db.Query(s, limit, offset)
		if err != nil {
			log.Fatal(err)
			return
		}

		var nrows int
		qparams := fmt.Sprintf("offset=%d&limit=%d&outputfmt=%s", offset, limit, outputfmt)
		if outputfmt == "grid" {
			nrows = printFilesGrid(w, rows, login, qparams)
		} else {
			nrows = printFilesTable(w, rows, login, qparams)
		}

		// Previous and More links
		printPagingNav(w, fmt.Sprintf("/browsefiles?outputfmt=%s", outputfmt), offset, limit, nrows)
		printPageFoot(w)
	}
}

func printFilesGrid(w http.ResponseWriter, rows *sql.Rows, login *User, qparams string) int {
	fmt.Fprintf(w, "<div class=\"file-browser\">\n")
	nrows := 0
	for rows.Next() {
		var fileid int64
		var filename, folder, desc, createdt string
		var fileUser User
		rows.Scan(&fileid, &filename, &folder, &desc, &createdt, &fileUser.Userid, &fileUser.Username)
		tcreatedt, _ := time.Parse(time.RFC3339, createdt)

		fmt.Fprintf(w, "<article class=\"content file-item\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading doc-title\"><a href=\"/file/%d\">%s</a></h1>\n", fileid, filename)
		printFileByline(w, login, fileid, &fileUser, tcreatedt, qparams)

		ext := fileext(filename)

		descMarkup := parseMarkdown(desc)
		if isFileExtImg(ext) {
			fmt.Fprintf(w, "<div class=\"file-desc small\">\n")
		} else {
			fmt.Fprintf(w, "<div class=\"file-desc large\">\n")
		}
		fmt.Fprintf(w, descMarkup)
		fmt.Fprintf(w, "</div>\n")

		if isFileExtImg(ext) {
			fmt.Fprintf(w, "<p>\n")
			fmt.Fprintf(w, "<a href=\"/file/%d\"><img class=\"preview\" src=\"/file/%d\"></a>\n", fileid, fileid)
			fmt.Fprintf(w, "</p>\n")
		}

		fmt.Fprintf(w, "</article>\n")

		nrows++
	}
	fmt.Fprintf(w, "</div>\n")

	return nrows
}

func printFilesTable(w http.ResponseWriter, rows *sql.Rows, login *User, qparams string) int {
	fmt.Fprintf(w, "<table class=\"table narrow\">\n")
	fmt.Fprintf(w, "<thead>\n")
	fmt.Fprintf(w, "    <tr>\n")
	fmt.Fprintf(w, "        <th scope=\"col\" class=\"filename smalltext\">Filename</th>\n")
	fmt.Fprintf(w, "        <th scope=\"col\" class=\"info smalltext\">Uploader</th>\n")
	fmt.Fprintf(w, "        <th scope=\"col\" class=\"action smalltext\">Action</th>\n")
	fmt.Fprintf(w, "    </tr>\n")
	fmt.Fprintf(w, "</thead>\n")
	fmt.Fprintf(w, "<tbody>\n")
	nrows := 0
	for rows.Next() {
		var fileid int64
		var filename, folder, desc, createdt string
		var fileUser User
		rows.Scan(&fileid, &filename, &folder, &desc, &createdt, &fileUser.Userid, &fileUser.Username)
		tcreatedt, _ := time.Parse(time.RFC3339, createdt)
		screatedt := tcreatedt.Format("2 Jan 2006")

		fmt.Fprintf(w, "  <tr>\n")

		fmt.Fprintf(w, "    <td class=\"filename\">\n")
		fmt.Fprintf(w, "      <p class=\"flex-row margin-bottom\">")
		fmt.Fprintf(w, "        <span class=\"doc-title smalltext\"><a href=\"/file/%d\">%s</a></span>", fileid, filename)
		if folder != "" {
			fmt.Fprintf(w, "      <span class=\"smalltext italic\">/%s/</span>", folder)
		}
		fmt.Fprintf(w, "      </p>")
		fmt.Fprintf(w, "      <div class=\"finetext\">\n")
		//		if isFileExtImg(fileext(filename)) {
		//			fmt.Fprintf(w, "<img class=\"thumbnail\" style=\"float: right;\" src=\"/file/%d\">\n", fileid)
		//		}
		fmt.Fprintf(w, parseMarkdown(desc))
		fmt.Fprintf(w, "      </div>\n")
		fmt.Fprintf(w, "    </td>\n")

		fmt.Fprintf(w, "    <td class=\"info finetext\">\n")
		fmt.Fprintf(w, "      <ul class=\"line-menu\">\n")
		fmt.Fprintf(w, "        <li>%s</li>\n", fileUser.Username)
		fmt.Fprintf(w, "        <li>%s</li>\n", screatedt)
		fmt.Fprintf(w, "      </ul>\n")
		fmt.Fprintf(w, "    </td>\n")

		fmt.Fprintf(w, "    <td class=\"action finetext\">\n")
		fmt.Fprintf(w, "      <ul class=\"line-menu\">\n")
		fmt.Fprintf(w, "        <li><a href=\"/editfile?fileid=%d&%s\">Update</a></li>\n", fileid, qparams)
		fmt.Fprintf(w, "        <li><a href=\"/delfile?fileid=%d&%s\">Delete</a></li>\n", fileid, qparams)
		fmt.Fprintf(w, "      </ul>\n")
		fmt.Fprintf(w, "    </td>\n")
		fmt.Fprintf(w, "  </tr>\n")
		nrows++
	}
	fmt.Fprintf(w, "</tbody>\n")
	fmt.Fprintf(w, "</table>\n")

	return nrows
}

// Return filename extension. Ex. "image.png" returns "png", "file1" returns "".
func fileext(filename string) string {
	ss := strings.Split(filename, ".")
	if len(ss) < 2 {
		return ""
	}
	return ss[len(ss)-1]
}

func fileHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var qfilename, qfolder string
		fileid := parseFileid(r.URL.Path)
		if fileid == -1 {
			qfolder, qfilename = parseUrlFilepath(r.URL.Path)
			if qfilename == "" {
				log.Printf("display file: no fileid or filepath\n")
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		//login := getLoginUser(r, db)
		// todo: authenticate user?
		login := getLoginUser(r, db)
		if !hasPageAccess(login, querySite(db)) {
			http.Error(w, "login needed", 404)
			return
		}

		var row *sql.Row
		var filename, folder string
		var fileUserid int64
		var bsContent []byte
		if qfilename != "" {
			s := "SELECT title, folder, content, user_id FROM entry INNER JOIN file ON entry.entry_id = file.entry_id WHERE title = ? AND folder = ? AND thing = 2"
			row = db.QueryRow(s, qfilename, qfolder)
		} else {
			s := "SELECT title, folder, content, user_id FROM entry INNER JOIN file ON entry.entry_id = file.entry_id WHERE entry.entry_id = ? AND thing = 2"
			row = db.QueryRow(s, fileid)
		}

		err := row.Scan(&filename, &folder, &bsContent, &fileUserid)
		if err == sql.ErrNoRows {
			// file doesn't exist
			log.Printf("display file: file doesn't exist\n")
			http.Error(w, fmt.Sprintf("file doesn't exist"), 404)
			return
		} else if err != nil {
			log.Printf("display file: server error (%s)\n", err)
			http.Error(w, fmt.Sprintf("server error (%s)", err), 500)
			return
		}

		ext := fileext(filename)
		if ext == "" {
			w.Header().Set("Content-Type", "application")
		} else if ext == "png" || ext == "gif" || ext == "bmp" {
			w.Header().Set("Content-Type", fmt.Sprintf("image/%s", ext))
		} else if ext == "jpg" {
			w.Header().Set("Content-Type", fmt.Sprintf("image/jpeg"))
		} else {
			w.Header().Set("Content-Type", fmt.Sprintf("application/%s", ext))
		}

		_, err = w.Write(bsContent)
		if err != nil {
			log.Printf("display file: server error (%s)\n", err)
			http.Error(w, fmt.Sprintf("server error (%s)", err), 500)
			return
		}
	}
}

func uploadFileHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var successmsg string
		var filename, folder, desc string

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("upload file: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			for {
				file, header, err := r.FormFile("file")
				if file != nil {
					defer file.Close()
				}
				if header == nil {
					errmsg = "Please select a file to upload."
					break
				}
				if err != nil {
					log.Printf("uploadfile: IO error reading file: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				createdt := time.Now().Format(time.RFC3339)
				filename = r.FormValue("filename")
				folder = r.FormValue("folder")
				desc = r.FormValue("desc")
				if filename == "" {
					filename = header.Filename
				}
				// Strip out any leading or trailing "/" from path.
				// Ex. "/abc/dir/" becomes "abc/dir".
				folder = strings.TrimPrefix(folder, "/")
				folder = strings.TrimSuffix(folder, "/")

				bsContent, err := ioutil.ReadAll(file)
				if err != nil {
					log.Printf("uploadfile: IO error reading file: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				s := "INSERT INTO entry (thing, title, body, createdt, user_id) VALUES (?, ?, ?, ?, ?);"
				result, err := sqlexec(db, s, FILE, filename, desc, createdt, login.Userid)
				if err != nil {
					log.Printf("uploadfile: DB error inserting file entry: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				s = "INSERT INTO file (entry_id, folder, content) VALUES (?, ?, ?);"
				newid, err := result.LastInsertId()
				if err != nil {
					log.Printf("uploadfile: DB error LastInsertId() not supported: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}
				_, err = sqlexec(db, s, newid, folder, bsContent)
				if err != nil {
					log.Printf("uploadfile: DB error inserting file contents: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				// Successfully added file.
				filepath := filename
				if folder != "" {
					filepath = fmt.Sprintf("%s/%s", folder, filename)
				}
				link := fmt.Sprintf("<a href=\"/file/%s\">%s</a>", filepath, filepath)
				successmsg = fmt.Sprintf("Successfully added file: %s", link)
				filename = ""
				folder = ""
				desc = ""
				break
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/uploadfile/\" method=\"post\" enctype=\"multipart/form-data\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading\">Upload File</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		if successmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"success\">%s</p>\n", successmsg)
			fmt.Fprintf(w, "</div>\n")
		}

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"file\">select file</label>\n")
		fmt.Fprintf(w, "<input id=\"file\" name=\"file\" type=\"file\">\n")
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"filename\">filename</label>\n")
		fmt.Fprintf(w, "<input id=\"filename\" name=\"filename\" type=\"text\" size=\"50\" value=\"%s\">\n", filename)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"folder\">folder</label>\n")
		fmt.Fprintf(w, "<input id=\"folder\" name=\"folder\" type=\"text\" size=\"50\" value=\"%s\">\n", folder)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"desc\">description</label>\n")
		fmt.Fprintf(w, "<textarea id=\"desc\" name=\"desc\" rows=\"5\">%s</textarea>\n", desc)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">upload file</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func editFileHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string

		fileid := idtoi(r.FormValue("fileid"))
		if fileid == -1 {
			log.Printf("edit file: no fileid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		offset := atoi(r.FormValue("offset"))
		if offset <= 0 {
			offset = 0
		}
		limit := atoi(r.FormValue("limit"))
		if limit <= 0 {
			limit = SETTINGS_LIMIT
		}
		outputfmt := r.FormValue("outputfmt")
		if outputfmt == "" {
			outputfmt = "table"
		}

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("edit file: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		var filename, folder, desc string
		var bsContent []byte
		var fileUserid int64
		s := "SELECT title, folder, body, user_id FROM entry INNER JOIN file ON entry.entry_id = file.entry_id WHERE entry.entry_id = ? AND thing = 2"
		row := db.QueryRow(s, fileid)
		err := row.Scan(&filename, &folder, &desc, &fileUserid)
		if err == sql.ErrNoRows {
			log.Printf("edit file: fileid %d doesn't exist\n", fileid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		// Allow only creators or admin to edit the file.
		if login.Userid != fileUserid && login.Userid != ADMIN_ID {
			log.Printf("User '%s' doesn't have access to fileid %d\n", login.Username, fileid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			for {
				file, header, err := r.FormFile("file")
				if file != nil {
					defer file.Close()
				}
				if header != nil && err != nil {
					log.Printf("editfile: IO error reading file: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				filename = r.FormValue("filename")
				folder = r.FormValue("folder")
				desc = r.FormValue("desc")
				if filename == "" && header != nil {
					filename = header.Filename
				}
				if filename == "" {
					errmsg = "Specify a filename."
					break
				}
				// Strip out any leading or trailing "/" from path.
				// Ex. "/abc/dir/" becomes "abc/dir".
				folder = strings.TrimPrefix(folder, "/")
				folder = strings.TrimSuffix(folder, "/")

				if header != nil {
					bsContent, err = ioutil.ReadAll(file)
					if err != nil {
						log.Printf("editfile: IO error reading file: %s\n", err)
						errmsg = "A problem occured. Please try again."
						break
					}
					s := "UPDATE entry SET title = ?, desc = ?, WHERE entry_id = ? AND thing = 2"
					_, err = sqlexec(db, s, filename, desc, fileid)
					if err != nil {
						log.Printf("editfile: DB error updating file: %s\n", err)
						errmsg = "A problem occured. Please try again."
						break
					}

					s = "UPDATE file SET folder = ?, content = ? WHERE entry_id = ?"
					_, err = sqlexec(db, s, folder, bsContent, fileid)
					if err != nil {
						log.Printf("editfile: DB error updating file: %s\n", err)
						errmsg = "A problem occured. Please try again."
						break
					}
				} else {
					s := "UPDATE entry SET title = ?, body = ? WHERE entry_id = ? AND thing = 2"
					_, err = sqlexec(db, s, filename, desc, fileid)
					if err != nil {
						log.Printf("editfile: DB error updating file: %s\n", err)
						errmsg = "A problem occured. Please try again."
						break
					}

					s = "UPDATE file SET folder = ? WHERE entry_id = ?"
					_, err = sqlexec(db, s, folder, fileid)
					if err != nil {
						log.Printf("editfile: DB error updating file: %s\n", err)
						errmsg = "A problem occured. Please try again."
						break
					}
				}

				// Successfully updated file.
				http.Redirect(w, r, fmt.Sprintf("/browsefiles?offset=%d&limit=%d&outputfmt=%s", offset, limit, outputfmt), http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/editfile/?fileid=%d&offset=%d&limit=%d&outputfmt=%s\" method=\"post\" enctype=\"multipart/form-data\">\n", fileid, offset, limit, outputfmt)
		fmt.Fprintf(w, "<h1 class=\"heading\">Edit File</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label>current file:</label>\n")
		fmt.Fprintf(w, "<a href=\"/file/%d\">%s</a>\n", fileid, filename)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"file\">replace file</label>\n")
		fmt.Fprintf(w, "<input id=\"file\" name=\"file\" type=\"file\">\n")
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"filename\">filename</label>\n")
		fmt.Fprintf(w, "<input id=\"filename\" name=\"filename\" type=\"text\" size=\"50\" value=\"%s\">\n", filename)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"folder\">folder</label>\n")
		fmt.Fprintf(w, "<input id=\"folder\" name=\"folder\" type=\"text\" size=\"50\" value=\"%s\">\n", folder)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"desc\">description</label>\n")
		fmt.Fprintf(w, "<textarea id=\"desc\" name=\"desc\" rows=\"5\">%s</textarea>\n", desc)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">update file</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func delFileHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string

		fileid := idtoi(r.FormValue("fileid"))
		if fileid == -1 {
			log.Printf("del file: no fileid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		offset := atoi(r.FormValue("offset"))
		if offset <= 0 {
			offset = 0
		}
		limit := atoi(r.FormValue("limit"))
		if limit <= 0 {
			limit = SETTINGS_LIMIT
		}
		outputfmt := r.FormValue("outputfmt")
		if outputfmt == "" {
			outputfmt = "table"
		}

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("del file: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		var filename, folder, desc string
		var fileUserid int64
		s := "SELECT title, folder, body, user_id FROM entry INNER JOIN file ON entry.entry_id = file.entry_id WHERE entry.entry_id = ? AND thing = 2"
		row := db.QueryRow(s, fileid)
		err := row.Scan(&filename, &folder, &desc, &fileUserid)
		if err == sql.ErrNoRows {
			log.Printf("del file: fileid %d doesn't exist\n", fileid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		// Allow only creators or admin to delete the file.
		if login.Userid != fileUserid && login.Userid != ADMIN_ID {
			log.Printf("User '%s' doesn't have access to fileid %d\n", login.Username, fileid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			for {
				s := "DELETE FROM entry WHERE entry_id = ? AND thing = 2"
				_, err = sqlexec(db, s, fileid)
				if err != nil {
					log.Printf("del file: DB error deleting file: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				s = "DELETE FROM file WHERE entry_id = ?"
				_, err = sqlexec(db, s, fileid)
				if err != nil {
					log.Printf("del file: DB error deleting file: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				// Successfully deleted file.
				http.Redirect(w, r, fmt.Sprintf("/browsefiles?offset=%d&limit=%d&outputfmt=%s", offset, limit, outputfmt), http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/delfile/?fileid=%d&offset=%d&limit=%d&outputfmt=%s\" method=\"post\" enctype=\"multipart/form-data\">\n", fileid, offset, limit, outputfmt)
		fmt.Fprintf(w, "<h1 class=\"heading warning\">Delete File</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label>current file:</label>\n")
		fmt.Fprintf(w, "<a href=\"/file/%d\">%s</a>\n", fileid, filename)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"filename\">filename</label>\n")
		fmt.Fprintf(w, "<input id=\"filename\" name=\"filename\" type=\"text\" size=\"50\" readonly value=\"%s\">\n", filename)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"folder\">folder</label>\n")
		fmt.Fprintf(w, "<input id=\"folder\" name=\"folder\" type=\"text\" size=\"50\" readonly value=\"%s\">\n", folder)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"desc\">description</label>\n")
		fmt.Fprintf(w, "<textarea id=\"desc\" name=\"desc\" rows=\"5\" readonly>%s</textarea>\n", desc)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">delete file</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func newReplyHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var replybody string

		noteid := idtoi(r.FormValue("noteid"))
		if noteid == -1 {
			log.Printf("new reply: no noteid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		login := getLoginUser(r, db)
		site := querySite(db)
		if login.Userid == -1 && site.AllowAnonReplies {
			login = queryUser(db, GUEST_ID)
		}
		if login.Userid == -1 || !login.Active {
			log.Printf("new reply: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			replybody = r.FormValue("replybody")
			replybody = strings.ReplaceAll(replybody, "\r", "") // CRLF => CR
			createdt := time.Now().Format(time.RFC3339)

			s := "INSERT INTO entry (thing, parent_id, title, body, createdt, user_id) VALUES (?, ?, ?, ?, ?, ?)"
			_, err := sqlexec(db, s, REPLY, noteid, "", replybody, createdt, login.Userid)
			if err != nil {
				log.Printf("DB error creating reply: %s\n", err)
				errmsg = "A problem occured. Please try again."
			}

			if errmsg == "" {
				http.Redirect(w, r, fmt.Sprintf("/note/%d", noteid), http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		if login.Mdeditor == SIMPLEMDE_EDIT {
			printPageHead(w, []string{
				"/static/simplemde.min.js",
			}, []string{
				"/static/simplemde.min.css",
			})
		} else {
			printPageHead(w, nil, nil)
		}
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		// Reply re-entry form
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/newreply/?noteid=%d\" method=\"post\">\n", noteid)
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"replybody\">enter reply:</label>\n")
		fmt.Fprintf(w, "<textarea id=\"replybody\" name=\"replybody\" rows=\"10\">%s</textarea>\n", replybody)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">add reply</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")

		if login.Mdeditor == SIMPLEMDE_EDIT {
			printSimpleMDECode(w, "replybody")
		}
		printPageFoot(w)
	}
}

func editReplyHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string

		replyid := idtoi(r.FormValue("replyid"))
		if replyid == -1 {
			log.Printf("edit reply: no noteid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("edit reply: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		var replybody string
		var replyUserid int64
		var noteid int64
		s := "SELECT body, user_id, parent_id FROM entry AS reply WHERE reply.entry_id = ? AND reply.thing = 1"
		row := db.QueryRow(s, replyid)
		err := row.Scan(&replybody, &replyUserid, &noteid)
		if err == sql.ErrNoRows {
			log.Printf("replyid %d doesn't exist\n", replyid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		// Allow only creators or admin to edit the reply.
		if login.Userid != replyUserid && login.Userid != ADMIN_ID {
			log.Printf("User '%s' doesn't have access to replyid %d\n", login.Username, replyid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			replybody = r.FormValue("replybody")
			replybody = strings.ReplaceAll(replybody, "\r", "") // CRLF => CR

			s := "UPDATE entry SET body = ? WHERE entry_id = ? AND thing = 1"
			_, err = sqlexec(db, s, replybody, replyid)
			if err != nil {
				log.Printf("DB error updating replyid %d: %s\n", replyid, err)
				errmsg = "A problem occured. Please try again."
			}

			if errmsg == "" {
				http.Redirect(w, r, fmt.Sprintf("/note/%d", noteid), http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		if login.Mdeditor == SIMPLEMDE_EDIT {
			printPageHead(w, []string{
				"/static/simplemde.min.js",
			}, []string{
				"/static/simplemde.min.css",
			})
		} else {
			printPageHead(w, nil, nil)
		}
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/editreply/?replyid=%d\" method=\"post\">\n", replyid)
		fmt.Fprintf(w, "<h1 class=\"heading\">Edit Reply</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<textarea id=\"replybody\" name=\"replybody\" rows=\"10\">%s</textarea>\n", replybody)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">update reply</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")

		fmt.Fprintf(w, "</article>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")

		if login.Mdeditor == SIMPLEMDE_EDIT {
			printSimpleMDECode(w, "replybody")
		}
		printPageFoot(w)
	}
}

func delReplyHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string

		replyid := idtoi(r.FormValue("replyid"))
		if replyid == -1 {
			log.Printf("del reply: no noteid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("del reply: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		var replybody string
		var replyUserid int64
		var noteid int64
		s := "SELECT body, user_id, parent_id FROM entry AS reply WHERE reply.entry_id = ? AND thing = 1"
		row := db.QueryRow(s, replyid)
		err := row.Scan(&replybody, &replyUserid, &noteid)
		if err == sql.ErrNoRows {
			log.Printf("replyid %d doesn't exist\n", replyid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		if err != nil {
			log.Fatal(err)
		}

		// Allow only creators or admin to delete the reply.
		if login.Userid != replyUserid && login.Userid != ADMIN_ID {
			log.Printf("User '%s' doesn't have access to replyid %d\n", login.Username, replyid)
			http.Redirect(w, r, fmt.Sprintf("/note/%d", noteid), http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			s := "DELETE FROM entry WHERE entry_id = ? AND thing = 1"
			_, err = sqlexec(db, s, replyid)
			if err != nil {
				log.Printf("DB error deleting replyid %d: %s\n", replyid, err)
				errmsg = "A problem occured. Please try again."
			}
			if errmsg == "" {
				http.Redirect(w, r, fmt.Sprintf("/note/%d", noteid), http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform displayonly\" action=\"/delreply/?replyid=%d\" method=\"post\">\n", replyid)
		fmt.Fprintf(w, "<h1 class=\"heading warning\">Delete Reply</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<textarea name=\"replybody\" rows=\"10\" readonly>%s</textarea>\n", replybody)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit warning\">delete reply</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func searchHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)
		if !hasPageAccess(login, querySite(db)) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
		}

		var errmsg string
		q := strings.TrimSpace(r.FormValue("q"))

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<h1 class=\"heading\">Search</h1>")
		fmt.Fprintf(w, "<form class=\"simpleform flex\" action=\"/search/\" method=\"get\">\n")

		fmt.Fprintf(w, "<input class=\"stretchwidth\" name=\"q\" placeholder=\"search\" value=\"%s\">\n", html.EscapeString(q))
		fmt.Fprintf(w, "<button class=\"submit\">Search</button>\n")
		fmt.Fprintf(w, "</form>\n")

		var rows *sql.Rows
		var err error
		if q != "" {
			s := `SELECT e.entry_id, e.title, snippet(fts, 1, '<span class="highlight">', '</span>', '...', 55) , u.username, e.createdt, e.thing, parent.title, e.parent_id 
FROM fts 
INNER JOIN entry AS e ON e.entry_id = fts.entry_id 
LEFT OUTER JOIN user AS u ON u.user_id = e.user_id 
LEFT OUTER JOIN entry AS parent ON e.parent_id = parent.entry_id
WHERE fts MATCH ? 
ORDER BY fts.rank`
			qfts := parseSearchQ(q)
			rows, err = db.Query(s, qfts)
			if err != nil {
				log.Printf("DB error on search: %s\n", err)
				errmsg = "A problem occured. Please try again."

				fmt.Fprintf(w, "<div class=\"control\">\n")
				fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
				fmt.Fprintf(w, "</div>\n")
			}
		}

		if rows != nil {
			printSearchResultsTable(w, rows)
		}
		printPageFoot(w)
	}
}

// Convert search string into an fts query string.
// Ex: '"this is a phrase" word1 word2' => '"this is a phrase" OR "word1" OR "word2"'
func parseSearchQ(q string) string {
	toks := tokenizeSearchQ(q)
	for i, tok := range toks {
		toks[i] = fmt.Sprintf("\"%s\"", tok)
	}
	return strings.Join(toks, " OR ")
}

// Tokenize search string before passing it to fts5,
// Ex: '"this is a phrase" word1 word2' => ["this is a phrase", "word1", "word2"]
func tokenizeSearchQ(q string) []string {
	var toks []string
	openquote := ""
	var tokb strings.Builder
	for _, c := range q {
		ch := string(c)

		// end quote: "abc def"
		if ch == openquote {
			tok := tokb.String()
			if tok != "" {
				toks = append(toks, tokb.String())
			}
			tokb.Reset()
			openquote = ""
			continue
		}
		// within quotes: "abc
		if openquote != "" {
			tokb.WriteString(ch)
			continue
		}
		// open quote: "
		if ch == "\"" || ch == "'" {
			openquote = ch
			continue
		}
		// whitespace (end token)
		if ch == " " || ch == "\t" || ch == "\n" || ch == "\r" {
			tok := tokb.String()
			if tok != "" {
				toks = append(toks, tokb.String())
			}
			tokb.Reset()
			continue
		}

		tokb.WriteString(ch)
	}

	if tokb.Len() > 0 {
		toks = append(toks, tokb.String())
		tokb.Reset()
	}
	return toks
}

func printSearchResultsTable(w http.ResponseWriter, rows *sql.Rows) {
	fmt.Fprintf(w, "<table class=\"table narrow\">\n")
	fmt.Fprintf(w, "<thead>\n")
	fmt.Fprintf(w, "    <tr>\n")
	fmt.Fprintf(w, "        <th scope=\"col\" class=\"title smalltext\">Title</th>\n")
	fmt.Fprintf(w, "        <th scope=\"col\" class=\"info smalltext\">Uploader</th>\n")
	fmt.Fprintf(w, "    </tr>\n")
	fmt.Fprintf(w, "</thead>\n")
	fmt.Fprintf(w, "<tbody>\n")

	//s := `SELECT fts.thing, fts.thing_id, fts.title, fts.body, user.username
	for rows.Next() {
		var entryid, parentid, thing int64
		var title, body, username, createdt, parentTitle string
		rows.Scan(&entryid, &title, &body, &username, &createdt, &thing, &parentTitle, &parentid)
		tcreatedt, _ := time.Parse(time.RFC3339, createdt)
		screatedt := tcreatedt.Format("2 Jan 2006")

		fmt.Fprintf(w, "  <tr>\n")

		fmt.Fprintf(w, "    <td class=\"title\">\n")
		fmt.Fprintf(w, "      <p class=\"flex-row2 margin-bottom\">")
		var link string
		var sthing string
		if thing == FILE {
			link = fmt.Sprintf("<a href=\"/file/%d\">%s</a>", entryid, title)
			sthing = "(file)"
		} else if thing == REPLY {
			link = fmt.Sprintf("<a href=\"/note/%d#reply-%d\">re: %s</a>", parentid, entryid, parentTitle)
			sthing = "(reply)"
		} else {
			link = fmt.Sprintf("<a href=\"/note/%d\">%s</a>", entryid, title)
			sthing = "(note)"
		}
		fmt.Fprintf(w, "        <span class=\"doc-title smalltext\">%s</span> <span class=\"finetext italic\">%s</a>", link, sthing)
		fmt.Fprintf(w, "      </p>")
		fmt.Fprintf(w, "      <div class=\"compact finetext\">%s</div>", parseMarkdown(body))
		fmt.Fprintf(w, "    </td>\n")

		fmt.Fprintf(w, "    <td class=\"info finetext\">\n")
		fmt.Fprintf(w, "      <ul class=\"line-menu\">\n")
		fmt.Fprintf(w, "        <li>%s</li>\n", username)
		fmt.Fprintf(w, "        <li>%s</li>\n", screatedt)
		fmt.Fprintf(w, "      </ul>\n")
		fmt.Fprintf(w, "    </td>\n")

		fmt.Fprintf(w, "  </tr>\n")
	}
	fmt.Fprintf(w, "</tbody>\n")
	fmt.Fprintf(w, "</table>\n")
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

func hashPassword(pwd string) string {
	hashedpwd, err := bcrypt.GenerateFromPassword([]byte(pwd), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return string(hashedpwd)
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
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		site := querySite(db)
		if strings.TrimSpace(site.Loginmsg) != "" {
			fmt.Fprintf(w, "<div class=\"simplebox compact spacedown\">\n")
			fmt.Fprintf(w, parseMarkdown(site.Loginmsg))
			fmt.Fprintf(w, "</div>\n")
		}

		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/login/\" method=\"post\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading\">Log In</h1>")
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

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func adminsetupHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		login := getLoginUser(r, db)
		if login.Userid != ADMIN_ID {
			log.Printf("adminsetup: admin not logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading\">Admin Setup</h1>\n")

		fmt.Fprintf(w, "<ul class=\"vertical-list\">\n")
		fmt.Fprintf(w, "  <li>\n")
		fmt.Fprintf(w, "    <p><a href=\"/sitesettings/\">Site Settings</a></p>\n")
		fmt.Fprintf(w, "    <p class=\"finetext\">Set site title and description.</p>\n")
		fmt.Fprintf(w, "  </li>\n")
		fmt.Fprintf(w, "  <li>\n")
		fmt.Fprintf(w, "    <p><a href=\"/userssetup/\">Users</a></p>\n")
		fmt.Fprintf(w, "    <p class=\"finetext\">Set usernames and passwords.</p>\n")
		fmt.Fprintf(w, "  </li>\n")
		fmt.Fprintf(w, "</ul>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func usersettingsHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var mdeditor int

		login := getLoginUser(r, db)
		if login.Userid == -1 || !login.Active {
			log.Printf("usersettings: no user or inactive user logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			mdeditor = SIMPLEMDE_EDIT
			if r.FormValue("useplaintextmdedit") != "" {
				mdeditor = TEXTAREA_EDIT
			}

			for {
				s := "UPDATE user SET mdeditor = ? WHERE user_id = ?"
				_, err := sqlexec(db, s, mdeditor, login.Userid)
				if err != nil {
					log.Printf("DB error updating user: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<h2 class=\"heading doc-title\">User Settings</h2>\n")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<ul class=\"vertical-list light-text\">\n")
		fmt.Fprintf(w, "  <li><a href=\"/edituser?userid=%d\">change username</a>\n", login.Userid)
		fmt.Fprintf(w, "  <li><a href=\"/edituser?userid=%d&setpwd=1\">change password</a>\n", login.Userid)
		fmt.Fprintf(w, "</ul>\n")

		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/usersettings/\" method=\"post\">\n")
		fmt.Fprintf(w, "<div class=\"control row\">\n")
		if login.Mdeditor == TEXTAREA_EDIT {
			fmt.Fprintf(w, "<input id=\"useplaintextmdedit\" name=\"useplaintextmdedit\" type=\"checkbox\" value=\"1\" checked>\n")
		} else {
			fmt.Fprintf(w, "<input id=\"useplaintextmdedit\" name=\"useplaintextmdedit\" type=\"checkbox\" value=\"1\">\n")
		}
		fmt.Fprintf(w, "<label for=\"useplaintextmdedit\">Use plain text editor for notes and replies</label>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">apply</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func newUserHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var username, password, password2 string

		login := getLoginUser(r, db)
		if login.Userid != ADMIN_ID {
			log.Printf("new user: admin not logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			username = r.FormValue("username")
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
				s := "INSERT INTO user (username, password, active, mdeditor) VALUES (?, ?, ?, ?);"
				_, err := sqlexec(db, s, username, hashedPassword, 1, 0)
				if err != nil {
					log.Printf("DB error creating user: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				http.Redirect(w, r, "/userssetup", http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/newuser/\" method=\"post\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading\">Create User</h1>")
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
		fmt.Fprintf(w, "<input id=\"password\" name=\"password\" type=\"password\" size=\"30\" value=\"%s\">\n", password)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"password2\">re-enter password</label>\n")
		fmt.Fprintf(w, "<input id=\"password2\" name=\"password2\" type=\"password\" size=\"30\" value=\"%s\">\n", password2)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">add user</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func editUserHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var username, password, password2 string

		setpwd := r.FormValue("setpwd") // ?setpwd=1 to prompt for new password
		userid := idtoi(r.FormValue("userid"))
		if userid == -1 {
			log.Printf("edit user: no userid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		login := getLoginUser(r, db)
		if login.Userid != ADMIN_ID && login.Userid != userid {
			log.Printf("edit user: admin or self user not logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		s := "SELECT username FROM user WHERE user_id = ?"
		row := db.QueryRow(s, userid)
		err := row.Scan(&username)
		if err == sql.ErrNoRows {
			// user doesn't exist
			log.Printf("userid %d doesn't exist\n", userid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			oldUsername := username
			username = strings.TrimSpace(r.FormValue("username"))

			for {
				// If username was changed,
				// make sure the new username hasn't been taken yet.
				if username != oldUsername && isUsernameExists(db, username) {
					errmsg = fmt.Sprintf("username '%s' already exists", username)
					break
				}

				if username == "" {
					errmsg = "Please enter a username."
					break
				}

				var err error
				if setpwd == "" {
					s := "UPDATE user SET username = ? WHERE user_id = ?"
					_, err = sqlexec(db, s, username, userid)
				} else {
					// ?setpwd=1 to set new password
					password = r.FormValue("password")
					password2 = r.FormValue("password2")
					if password != password2 {
						errmsg = "re-entered password doesn't match"
						password = ""
						password2 = ""
						break
					}
					hashedPassword := hashPassword(password)
					s := "UPDATE user SET password = ? WHERE user_id = ?"
					_, err = sqlexec(db, s, hashedPassword, userid)
				}
				if err != nil {
					log.Printf("DB error updating user: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				if login.Userid == ADMIN_ID {
					http.Redirect(w, r, "/userssetup/", http.StatusSeeOther)
				} else {
					http.Redirect(w, r, "/usersettings/", http.StatusSeeOther)
				}
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/edituser/?userid=%d&setpwd=%s\" method=\"post\">\n", userid, setpwd)
		fmt.Fprintf(w, "<h1 class=\"heading\">Edit User</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		// ?setpwd=1 to set new password
		if setpwd != "" {
			fmt.Fprintf(w, "<div class=\"control displayonly\">\n")
			fmt.Fprintf(w, "<label>username</label>\n")
			fmt.Fprintf(w, "<input name=\"username\" type=\"text\" size=\"20\" value=\"%s\" readonly>\n", username)
			fmt.Fprintf(w, "</div>\n")

			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<label>password</label>\n")
			fmt.Fprintf(w, "<input name=\"password\" type=\"password\" size=\"30\" value=\"%s\">\n", password)
			fmt.Fprintf(w, "</div>\n")

			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<label>re-enter password</label>\n")
			fmt.Fprintf(w, "<input name=\"password2\" type=\"password\" size=\"30\" value=\"%s\">\n", password2)
			fmt.Fprintf(w, "</div>\n")
		} else {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<label>username</label>\n")
			fmt.Fprintf(w, "<input name=\"username\" type=\"text\" size=\"20\" value=\"%s\">\n", username)
			fmt.Fprintf(w, "</div>\n")
		}

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">update user</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func activateUserHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string
		var username string

		setactive := atoi(r.FormValue("setactive"))
		if setactive != 0 && setactive != 1 {
			log.Printf("activate user: setactive should be 0 or 1\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		userid := idtoi(r.FormValue("userid"))
		if userid == -1 {
			log.Printf("activate user: no userid\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		login := getLoginUser(r, db)
		if login.Userid != ADMIN_ID {
			log.Printf("activate user: admin not logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		s := "SELECT username FROM user WHERE user_id = ?"
		row := db.QueryRow(s, userid)
		err := row.Scan(&username)
		if err == sql.ErrNoRows {
			// user doesn't exist
			log.Printf("userid %d doesn't exist\n", userid)
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		if r.Method == "POST" {
			for {
				var err error
				s := "UPDATE user SET active = ? WHERE user_id = ?"
				_, err = sqlexec(db, s, setactive, userid)
				if err != nil {
					log.Printf("DB error updating user.active: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				http.Redirect(w, r, "/userssetup/", http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/activateuser/?userid=%d&setactive=%d\" method=\"post\">\n", userid, setactive)
		if setactive == 0 {
			fmt.Fprintf(w, "<h1 class=\"heading\">Deactivate User</h1>")
		} else {
			fmt.Fprintf(w, "<h1 class=\"heading\">Activate User</h1>")
		}
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label>username</label>\n")
		fmt.Fprintf(w, "<input name=\"username\" type=\"text\" size=\"20\" readonly value=\"%s\">\n", username)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		if setactive == 0 {
			fmt.Fprintf(w, "<button class=\"submit\">deactivate user</button>\n")
		} else {
			fmt.Fprintf(w, "<button class=\"submit\">activate user</button>\n")
		}
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func sitesettingsHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string

		login := getLoginUser(r, db)
		if login.Userid != ADMIN_ID {
			log.Printf("sitesettings: admin not logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		site := querySite(db)

		if r.Method == "POST" {
			site.Title = r.FormValue("title")
			site.Desc = r.FormValue("desc")
			site.Loginmsg = r.FormValue("loginmsg")
			site.Sidebar1 = r.FormValue("sidebar1")
			site.Sidebar2 = r.FormValue("sidebar2")
			site.RequireLoginForPageview = false
			if r.FormValue("requireloginforpageview") != "" {
				site.RequireLoginForPageview = true
			}
			site.AllowAnonReplies = false
			if r.FormValue("allowanonreplies") != "" {
				site.AllowAnonReplies = true
			}

			for {
				requireloginforpageview := 0
				if site.RequireLoginForPageview {
					requireloginforpageview = 1
				}
				allowanonreplies := 0
				if site.AllowAnonReplies {
					allowanonreplies = 1
				}
				s := "INSERT OR REPLACE INTO site (site_id, title, desc, requireloginforpageview, allowanonreplies, loginmsg, sidebar1, sidebar2) VALUES (1, ?, ?, ?, ?, ?, ?, ?)"
				_, err := sqlexec(db, s, site.Title, site.Desc, requireloginforpageview, allowanonreplies, site.Loginmsg, site.Sidebar1, site.Sidebar2)
				if err != nil {
					log.Printf("DB error updating site settings: %s\n", err)
					errmsg = "A problem occured. Please try again."
					break
				}

				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)

		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<form class=\"simpleform\" action=\"/sitesettings/\" method=\"post\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading\">Site Settings</h1>")
		if errmsg != "" {
			fmt.Fprintf(w, "<div class=\"control\">\n")
			fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
			fmt.Fprintf(w, "</div>\n")
		}
		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"title\">site title</label>\n")
		fmt.Fprintf(w, "<input id=\"title\" name=\"title\" type=\"text\" size=\"50\" value=\"%s\">\n", site.Title)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"desc\">site description</label>\n")
		fmt.Fprintf(w, "<input id=\"desc\" name=\"desc\" type=\"text\" size=\"50\" value=\"%s\">\n", site.Desc)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control row\">\n")
		if site.RequireLoginForPageview {
			fmt.Fprintf(w, "<input id=\"requireloginforpageview\" name=\"requireloginforpageview\" type=\"checkbox\" value=\"1\" checked>\n")
		} else {
			fmt.Fprintf(w, "<input id=\"requireloginforpageview\" name=\"requireloginforpageview\" type=\"checkbox\" value=\"1\">\n")
		}
		fmt.Fprintf(w, "<label for=\"requireloginforpageview\">Require login to view pages</label>\n")
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control row\">\n")
		if site.AllowAnonReplies {
			fmt.Fprintf(w, "<input id=\"allowanonreplies\" name=\"allowanonreplies\" type=\"checkbox\" value=\"1\" checked>\n")
		} else {
			fmt.Fprintf(w, "<input id=\"allowanonreplies\" name=\"allowanonreplies\" type=\"checkbox\" value=\"1\">\n")
		}
		fmt.Fprintf(w, "<label for=\"allowanonreplies\">Allow anonymous replies</label>\n")
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"loginmsg\">Additional Login Message</label>\n")
		fmt.Fprintf(w, "<textarea id=\"loginmsg\" name=\"loginmsg\" rows=\"10\">%s</textarea>\n", site.Loginmsg)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"loginmsg\">Sidebar 1 Section</label>\n")
		fmt.Fprintf(w, "<textarea id=\"sidebar1\" name=\"sidebar1\" rows=\"10\">%s</textarea>\n", site.Sidebar1)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<label for=\"loginmsg\">Sidebar 2 Section</label>\n")
		fmt.Fprintf(w, "<textarea id=\"sidebar2\" name=\"sidebar2\" rows=\"10\">%s</textarea>\n", site.Sidebar2)
		fmt.Fprintf(w, "</div>\n")

		fmt.Fprintf(w, "<div class=\"control\">\n")
		fmt.Fprintf(w, "<button class=\"submit\">update settings</button>\n")
		fmt.Fprintf(w, "</div>\n")
		fmt.Fprintf(w, "</form>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func userssetupHandler(db *sql.DB) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		var errmsg string

		login := getLoginUser(r, db)
		if login.Userid != ADMIN_ID {
			log.Printf("userssetup: admin not logged in\n")
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		printPageHead(w, nil, nil)
		printPageNav(w, r, db)
		fmt.Fprintf(w, "<div class=\"main\">\n")
		fmt.Fprintf(w, "<section class=\"main-content\">\n")
		fmt.Fprintf(w, "<h1 class=\"heading\">Users Setup</h1>\n")
		fmt.Fprintf(w, "<ul class=\"vertical-list\">\n")
		fmt.Fprintf(w, "<li>\n")
		fmt.Fprintf(w, "  <ul class=\"line-menu finetext\">\n")
		fmt.Fprintf(w, "    <li><p><a href=\"/newuser/\">Create new user</a></p></li>\n")
		fmt.Fprintf(w, "  </ul>\n")
		fmt.Fprintf(w, "</li>\n")
		s := "SELECT user_id, username, active, mdeditor FROM user ORDER BY active DESC, username"
		rows, err := db.Query(s)
		for {
			if err != nil {
				errmsg = "A problem occured while loading users. Please try again."
				fmt.Fprintf(w, "<li>\n")
				fmt.Fprintf(w, "<p class=\"error\">%s</p>\n", errmsg)
				fmt.Fprintf(w, "</li>\n")
				break
			}

			for rows.Next() {
				var u User
				rows.Scan(&u.Userid, &u.Username, &u.Active, &u.Mdeditor)
				fmt.Fprintf(w, "<li>\n")
				if u.Active {
					fmt.Fprintf(w, "<p>%s</p>\n", u.Username)
				} else {
					fmt.Fprintf(w, "<p class=\"greyed-text\">(%s)</p>\n", u.Username)
				}

				fmt.Fprintf(w, "<ul class=\"line-menu finetext\">\n")
				fmt.Fprintf(w, "  <li><a href=\"/edituser?userid=%d\">change username</a>\n", u.Userid)
				fmt.Fprintf(w, "  <li><a href=\"/edituser?userid=%d&setpwd=1\">set password</a>\n", u.Userid)

				if u.Userid != ADMIN_ID {
					if u.Active {
						fmt.Fprintf(w, "  <li><a href=\"/activateuser?userid=%d&setactive=0\">deactivate</a>\n", u.Userid)
					} else {
						fmt.Fprintf(w, "  <li><a href=\"/activateuser?userid=%d&setactive=1\">activate</a>\n", u.Userid)
					}
				}
				fmt.Fprintf(w, "</ul>\n")

				fmt.Fprintf(w, "</li>\n")
			}
			break
		}

		fmt.Fprintf(w, "</ul>\n")
		fmt.Fprintf(w, "</section>\n")

		printPageSidebar(db, w, querySite(db))
		fmt.Fprintf(w, "</div>\n")
		printPageFoot(w)
	}
}

func printByline(w io.Writer, login *User, noteid int64, noteUser *User, tcreatedt time.Time, nreplies int) {
	createdt := tcreatedt.Format("2 Jan 2006")
	fmt.Fprintf(w, "<ul class=\"line-menu finetext\">\n")
	fmt.Fprintf(w, "<li>%s</li>\n", createdt)
	fmt.Fprintf(w, "<li>%s</li>\n", noteUser.Username)
	if nreplies > 0 {
		if nreplies == 1 {
			fmt.Fprintf(w, "<li>(%d reply)</li>\n", nreplies)
		} else {
			fmt.Fprintf(w, "<li>(%d replies)</li>\n", nreplies)
		}
	}
	// Show edit/delete links if admin or active user owner of note.
	if login.Userid == ADMIN_ID || (noteUser.Userid == login.Userid && login.Active) {
		fmt.Fprintf(w, "<li><a href=\"/editnote/?noteid=%d\">Edit</a></li>\n", noteid)
		fmt.Fprintf(w, "<li><a href=\"/delnote/?noteid=%d\">Delete</a></li>\n", noteid)
	}
	fmt.Fprintf(w, "</ul>\n")
}

func printFileByline(w io.Writer, login *User, fileid int64, fileUser *User, tcreatedt time.Time, qparams string) {
	createdt := tcreatedt.Format("2 Jan 2006")
	fmt.Fprintf(w, "<ul class=\"line-menu finetext\">\n")
	fmt.Fprintf(w, "<li>%s</li>\n", createdt)
	fmt.Fprintf(w, "<li>%s</li>\n", fileUser.Username)
	fmt.Fprintf(w, "</ul>\n")

	// Show edit/delete links if admin or active user owner of file.
	if login.Userid == ADMIN_ID || (fileUser.Userid == login.Userid && login.Active) {
		fmt.Fprintf(w, "<ul class=\"line-menu finetext\">\n")
		fmt.Fprintf(w, "<li><a href=\"/editfile/?fileid=%d&%s\">Update</a></li>\n", fileid, qparams)
		fmt.Fprintf(w, "<li><a href=\"/delfile/?fileid=%d&%s\">Delete</a></li>\n", fileid, qparams)
		fmt.Fprintf(w, "</ul>\n")
	}
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
}

func printPageFoot(w io.Writer) {
	fmt.Fprintf(w, "</body>\n")
	fmt.Fprintf(w, "</html>\n")
}

func printPageNav(w http.ResponseWriter, r *http.Request, db *sql.DB) {
	login := getLoginUser(r, db)

	notitle := "(No Title)"
	nodesc := "(Put a Description Here)"

	s := "SELECT title, desc FROM site WHERE site_id = 1"
	row := db.QueryRow(s)
	var title, desc string
	err := row.Scan(&title, &desc)
	if err != nil {
		title = notitle
		desc = nodesc
	}
	if strings.TrimSpace(title) == "" {
		title = notitle
	}
	if strings.TrimSpace(desc) == "" {
		desc = nodesc
	}

	fmt.Fprintf(w, "<header class=\"masthead\">\n")
	fmt.Fprintf(w, "<nav class=\"navbar\">\n")
	fmt.Fprintf(w, "<div>\n")

	// Menu row 1
	fmt.Fprintf(w, "<div>\n")
	fmt.Fprintf(w, "<h1><a href=\"/\">%s</a></h1>\n", title)
	fmt.Fprintf(w, "</div>\n")

	// Menu row 2
	fmt.Fprintf(w, "<div class=\"smalltext italic\">\n")
	if login.Userid != -1 && login.Active {
		fmt.Fprintf(w, "<a href=\"/createnote/\">create note</a>\n")
	}
	fmt.Fprintf(w, "<a href=\"/\">browse notes</a>\n")

	if login.Userid != -1 && login.Active {
		fmt.Fprintf(w, "<a href=\"/uploadfile/\">upload file</a>\n")
	}
	fmt.Fprintf(w, "<a href=\"/browsefiles/\">browse files</a>\n")
	fmt.Fprintf(w, "<a href=\"/search/\">search</a>\n")
	fmt.Fprintf(w, "</div>\n")

	fmt.Fprintf(w, "</div>\n")

	// User section (right part)
	fmt.Fprintf(w, "<div class=\"align-right\">\n")
	fmt.Fprintf(w, "<div class=\"\">\n")
	fmt.Fprintf(w, "<span>%s</span>\n", login.Username)
	fmt.Fprintf(w, "</div>\n")

	fmt.Fprintf(w, "<div>\n")
	fmt.Fprintf(w, "<span class=\"smalltext italic\">\n")
	if login.Userid == ADMIN_ID {
		fmt.Fprintf(w, "<a href=\"/adminsetup\">setup</a>\n")
	} else if login.Userid != -1 && login.Active {
		fmt.Fprintf(w, "<a href=\"/usersettings\">settings</a>\n")
	}
	if login.Userid != -1 {
		fmt.Fprintf(w, "<a href=\"/logout\">logout</a>\n")
	} else {
		fmt.Fprintf(w, "<a href=\"/login\">login</a>\n")
	}
	fmt.Fprintf(w, "</span>\n")
	fmt.Fprintf(w, "</div>\n")

	fmt.Fprintf(w, "</div>")
	fmt.Fprintf(w, "</nav>\n")

	// Site description line
	fmt.Fprintf(w, "<p class=\"finetext\">%s</p>\n", desc)
	fmt.Fprintf(w, "</header>\n")
}

func printPageSidebar(db *sql.DB, w http.ResponseWriter, site *Site) {
	if strings.TrimSpace(site.Sidebar1) == "" && strings.TrimSpace(site.Sidebar2) == "" {
		return
	}

	fmt.Fprintf(w, "<section class=\"main-sidebar\">\n")
	if strings.TrimSpace(site.Sidebar1) != "" {
		fmt.Fprintf(w, "<div class=\"sidebar-item compact spacedown\">\n")
		fmt.Fprintf(w, parseMarkdown(site.Sidebar1))
		fmt.Fprintf(w, "</div>\n")
	}
	if strings.TrimSpace(site.Sidebar2) != "" {
		fmt.Fprintf(w, "<div class=\"sidebar-item compact spacedown\">\n")
		fmt.Fprintf(w, parseMarkdown(site.Sidebar2))
		fmt.Fprintf(w, "</div>\n")
	}
	fmt.Fprintf(w, "</section>\n")
}

func printPagingNav(w http.ResponseWriter, baseurl string, offset, limit, nrows int) {
	fmt.Fprintf(w, "<div class=\"flex-row italic smalltext\">\n")
	if offset > 0 {
		prevOffset := offset - limit
		if prevOffset < 0 {
			prevOffset = 0
		}
		prevLink := fmt.Sprintf("%s&offset=%d&limit=%d", baseurl, prevOffset, limit)
		fmt.Fprintf(w, "  <p><a href=\"%s\">Previous</a></p>\n", prevLink)
	} else {
		fmt.Fprintf(w, "  <p></p>\n")
	}
	if nrows == limit {
		moreLink := fmt.Sprintf("%s&offset=%d&limit=%d", baseurl, offset+limit, limit)
		fmt.Fprintf(w, "  <p><a href=\"%s\">More</a></p>\n", moreLink)
	}
	fmt.Fprintf(w, "</div>\n")
}

func getLoginUser(r *http.Request, db *sql.DB) *User {
	c, err := r.Cookie("userid")
	if err != nil {
		return &User{-1, "", false, 0}
	}

	userid := idtoi(c.Value)
	if userid == -1 {
		return &User{-1, "", false, 0}
	}

	u := queryUser(db, userid)
	return u
}

func queryUser(db *sql.DB, userid int64) *User {
	var u User
	s := "SELECT user_id, username, active, mdeditor FROM user WHERE user_id = ?"
	row := db.QueryRow(s, userid)
	err := row.Scan(&u.Userid, &u.Username, &u.Active, &u.Mdeditor)
	if err == sql.ErrNoRows {
		return &User{-1, "", false, 0}
	}
	if err != nil {
		fmt.Printf("queryUser() db error (%s)\n", err)
		return &User{-1, "", false, 0}
	}
	return &u
}

func querySite(db *sql.DB) *Site {
	var site Site
	s := "SELECT title, desc, requireloginforpageview, allowanonreplies, loginmsg, sidebar1, sidebar2 FROM site WHERE site_id = 1"
	row := db.QueryRow(s)
	err := row.Scan(&site.Title, &site.Desc, &site.RequireLoginForPageview, &site.AllowAnonReplies, &site.Loginmsg, &site.Sidebar1, &site.Sidebar2)
	if err == sql.ErrNoRows {
		// Site settings row not defined yet, just use default Site values.
		site.Title = "Group Notes"
		site.Desc = "Central repository for notes"
		site.RequireLoginForPageview = true
		site.AllowAnonReplies = false
	} else if err != nil {
		// DB error, log then use strict Site settings.
		log.Printf("error reading site settings for siteid %d (%s)\n", 1, err)
		site.RequireLoginForPageview = true
		site.AllowAnonReplies = false
	}
	return &site
}

func hasPageAccess(u *User, site *Site) bool {
	if site.RequireLoginForPageview && (u.Userid <= 0 || !u.Active) {
		return false
	}
	return true
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

func queryPrevNoteid(db *sql.DB, noteid int64) (int64, string) {
	s := "SELECT entry_id, title FROM entry WHERE entry_id < ? AND thing = 0 ORDER BY entry_id DESC LIMIT 1"
	row := db.QueryRow(s, noteid)
	var prevNoteid int64
	var title string
	err := row.Scan(&prevNoteid, &title)
	if err == sql.ErrNoRows {
		return -1, ""
	}
	if err != nil {
		log.Printf("queryPrevNoteid(): noteid=%d, err: '%s'\n", noteid, err)
		return -1, ""
	}
	return prevNoteid, title
}

func queryNextNoteid(db *sql.DB, noteid int64) (int64, string) {
	s := "SELECT entry_id, title FROM entry WHERE entry_id > ? AND thing = 0 ORDER BY entry_id LIMIT 1"
	row := db.QueryRow(s, noteid)
	var nextNoteid int64
	var title string
	err := row.Scan(&nextNoteid, &title)
	if err == sql.ErrNoRows {
		return -1, ""
	}
	if err != nil {
		log.Printf("queryNextNoteid(): noteid=%d, err: '%s'\n", noteid, err)
		return -1, ""
	}
	return nextNoteid, title
}

func parseMarkdown(s string) string {
	return string(blackfriday.Run([]byte(s), blackfriday.WithExtensions(blackfriday.HardLineBreak)))
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
