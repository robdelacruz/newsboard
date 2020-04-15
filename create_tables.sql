BEGIN TRANSACTION;

DROP TABLE IF EXISTS user;
DROP TABLE IF EXISTS entry;
DROP TABLE IF EXISTS entryvote;
DROP TABLE IF EXISTS site;
DROP VIEW IF EXISTS totalvotes;

-- thing enum values:
--   submission = 0
--   reply = 1

-- tables: entry, user, entryvote
CREATE TABLE entry (entry_id INTEGER PRIMARY KEY NOT NULL, thing INTEGER NOT NULL DEFAULT 0, title TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '', body TEXT NOT NULL DEFAULT '', createdt TEXT NOT NULL, user_id INTEGER NOT NULL, parent_id INTEGER DEFAULT 0);

CREATE TABLE user (user_id INTEGER PRIMARY KEY NOT NULL, username TEXT, password TEXT, active INTEGER NOT NULL, email TEXT, CONSTRAINT unique_username UNIQUE (username));
INSERT INTO user (user_id, username, password, active, email) VALUES (1, 'admin', '', 1, 'admin@localhost');

CREATE TABLE entryvote(entry_id INTEGER NOT NULL, user_id INTEGER, PRIMARY KEY (entry_id, user_id));

CREATE TABLE site (site_id INTEGER PRIMARY KEY NOT NULL, title TEXT NOT NULL, desc TEXT NOT NULL, gravityf REAL NOT NULL);
INSERT INTO site (site_id, title, desc, gravityf) VALUES (1, 'newsboard', '', 1.0);

-- view: totalvotes
CREATE VIEW totalvotes 
AS 
SELECT entry_id, COUNT(*) AS votes FROM entryvote GROUP BY entry_id;

-- entry 1
INSERT INTO entry (entry_id, thing, title, url, body, createdt, user_id, parent_id) VALUES (1, 0, 'newsboard - a hackernews clone', 'https://github.com/robdelacruz/newsboard', '', strftime('%Y-%m-%dT%H:%M:%SZ', 'now'), 2, 0);

COMMIT;

