.PHONY: lint test build clean set-version

lint:
	golangci-lint run ./...

test:
	go test -v ./...

build:
	go build -o orobox main.go

clean:
	rm -f orobox

set-version:
	@if [ -z "$(v)" ]; then \
		echo "Usage: make set-version v=X.Y.Z"; \
		exit 1; \
	fi
	@CUR_VERSION=$$(grep 'var Version =' cmd/root.go | cut -d'"' -f2); \
	echo "Updating version from $$CUR_VERSION to $(v)..."; \
	sed -i 's/var Version = "'$$CUR_VERSION'"/var Version = "$(v)"/' cmd/root.go; \
	sed -i "s/$$CUR_VERSION/$(v)/g" README.md; \
	echo "Version updated successfully!"

pre-commit: lint test build