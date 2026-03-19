.PHONY: lint test build clean

lint:
	golangci-lint run ./...

test:
	go test -v ./...

build:
	go build -o orobox main.go

clean:
	rm -f orobox

pre-commit: lint test build