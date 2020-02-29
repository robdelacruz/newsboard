BEGIN TRANSACTION;

DROP TABLE IF EXISTS user;
DROP TABLE IF EXISTS entry;

-- thing enum values:
--   submission = 0
--   reply = 1

CREATE TABLE entry (entry_id INTEGER PRIMARY KEY NOT NULL, thing INTEGER NOT NULL DEFAULT 0, title TEXT NOT NULL DEFAULT '', url TEXT NOT NULL DEFAULT '', body TEXT NOT NULL, createdt TEXT NOT NULL, user_id INTEGER NOT NULL, parent_id INTEGER DEFAULT 0);

CREATE TABLE user (user_id INTEGER PRIMARY KEY NOT NULL, username TEXT, password TEXT, active INTEGER NOT NULL, email TEXT, CONSTRAINT unique_username UNIQUE (username), CONSTRAINT unique_email UNIQUE (email));
INSERT INTO user (user_id, username, password, active, email) VALUES (1, 'admin', '', 1, 'admin@localhost');

INSERT INTO user (user_id, username, password, active, email) VALUES (2, 'robdelacruz', '', 1, 'rob@localhost');
INSERT INTO user (user_id, username, password, active, email) VALUES (3, 'user1', '', 1, 'user1@localhost');
INSERT INTO user (user_id, username, password, active, email) VALUES (4, 'user2', '', 1, 'user2@localhost');

INSERT INTO entry (entry_id, thing, title, url, body, createdt, user_id, parent_id) VALUES (1, 0, 'newsboard - a hackernews clone', 'https://github.com/robdelacruz/newsboard', '', '2020-02-25T14:00:00+08:00', 2, 0);
INSERT INTO entry (entry_id, thing, title, url, body, createdt, user_id, parent_id) VALUES (2, 0, 'Ask NB: What programming language was used to develop NB?', '',
'I really like NB''s user interface and functionality. I''m curious as to what programming language and database was used to program it?
    
Also, how long did it take to code it and how much of the functionality was borrowed from HackerNews?
', '2020-02-25T14:00:00+08:00', 2, 0);

INSERT INTO entry (entry_id, thing, title, body, createdt, user_id, parent_id) VALUES (3, 1, '', 'I thought I read somewhere that NewsBoard was programmed in Go, with Sqlite3 as the database. Their code is even posted on github.', '2020-02-25T14:00:00+08:00', 3, 2);
INSERT INTO entry (entry_id, thing, title, body, createdt, user_id, parent_id) VALUES (4, 1, '', 'I bet they used Perl because the UI looks cool.', '2020-02-25T14:00:00+08:00', 4, 2);
INSERT INTO entry (entry_id, thing, title, body, createdt, user_id, parent_id) VALUES (5, 1, '', 'No, they used Go. Check out their [github](https://github.com/robdelacruz/newsboard) page.', '2020-02-25T14:00:00+08:00', 3, 4);

INSERT INTO entry (entry_id, thing, title, body, createdt, user_id, parent_id) VALUES (6, 1, '', 'NB''s interface looks oddly familiar. It''s like I saw it somewhere else before.', '2020-02-25T14:00:00+08:00', 1, 2);

INSERT INTO entry (entry_id, thing, title, body, createdt, user_id, parent_id) VALUES (7, 1, '', 'That''s because they stole the entire thing from HackerNews.', '2020-02-25T14:00:00+08:00', 2, 6);

INSERT INTO entry (entry_id, thing, title, body, createdt, user_id, parent_id) VALUES (8, 1, '', 
'Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.

Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.',
    '2020-02-25T14:00:00+08:00', 2, 5);

INSERT INTO entry (entry_id, thing, title, body, createdt, user_id, parent_id) VALUES (9, 1, '', 
'Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.

Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua.',
    '2020-02-25T14:00:00+08:00', 2, 8);
COMMIT;

