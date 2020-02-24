BEGIN TRANSACTION;

DROP TABLE IF EXISTS user;
DROP TABLE IF EXISTS entry;

-- thing enum values:
--   submission = 0
--   reply = 1

CREATE TABLE entry (entry_id INTEGER PRIMARY KEY NOT NULL, thing INTEGER NOT NULL, title TEXT, url TEXT, body TEXT, createdt TEXT, user_id INTEGER, parent_id INTEGER);

CREATE TABLE user (user_id INTEGER PRIMARY KEY NOT NULL, username TEXT, password TEXT, active INTEGER NOT NULL, email TEXT, CONSTRAINT unique_username UNIQUE (username), CONSTRAINT unique_email UNIQUE (email));
INSERT INTO user (user_id, username, password, active, email) VALUES (1, 'admin', '', 1, 'admin@localhost');

INSERT INTO user (user_id, username, password, active, email) VALUES (2, 'robdelacruz', '', 1, 'rob@localhost');
INSERT INTO entry (thing, title, url, body, createdt, user_id, parent_id) VALUES (0, 'newsboard - a hackernews clone', 'https://github.com/robdelacruz/newsboard', '', '2020-02-25T14:00:00+08:00', 2, 0);
INSERT INTO entry (thing, title, url, body, createdt, user_id, parent_id) VALUES (0, 'Ask NB: What programming language was used to develop NB?', '',
'I really like NB''s user interface and functionality. I''m curious as to what programming language and database was used to program it?
    
Also, how long did it take to code it and how much of the functionality was borrowed from HackerNews?
', '2020-02-25T14:00:00+08:00', 2, 0);

COMMIT;

