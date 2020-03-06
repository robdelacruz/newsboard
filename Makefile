all: nb initdata

dep:
	go get -u github.com/mattn/go-sqlite3
	go get -u golang.org/x/crypto/bcrypt
	go get -u gopkg.in/russross/blackfriday.v2

nb: nb.go
	go build -o nb nb.go

initdata: tools/initdata.go
	go build -o initdata tools/initdata.go

clean:
	rm -rf nb initdata

