all: newsboard

dep:
	go get -u github.com/mattn/go-sqlite3

newsboard: main.go
	go build -o newsboard newsboard.go

clean:
	rm -rf newsboard

