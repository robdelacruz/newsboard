all: nb

dep:
	go get -u github.com/mattn/go-sqlite3

nb: nb.go
	go build -o nb nb.go

clean:
	rm -rf nb

