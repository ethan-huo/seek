.PHONY: build install clean

build:
	CGO_ENABLED=1 go build -tags "fts5" -o seek .

install:
	CGO_ENABLED=1 go install -tags "fts5" .

clean:
	rm -f seek
