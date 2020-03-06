## About newsboard

newsboard is a bulletin board for posting stories and links. Inspired by HackerNews.

- Submit stories and links.
- Reply to stories.
- Multiple users.
- MIT License

## Build and Install

    $ make dep
    $ sqlite3 news.db < create_tables.sql
    $ make

    Run 'nb <news.db>' to start the web service.

newsboard uses a single sqlite3 database file to store all submissions, users, and site settings.

Still under development.

## Screenshots

![newsboard list](screenshots/nb-index.png)
![newsboard item](screenshots/nb-item1.png)

## Contact
    Twitter: @robdelacruz
    Source: http://github.com/robdelacruz/newsboard

