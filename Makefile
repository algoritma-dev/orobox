GOPATH := $(shell go env GOPATH)
PATH := $(PATH):$(GOPATH)/bin

.PHONY: lint test build clean set-version e2e docker-image

# Image coordinates for `docker-image`. Override on the command line, e.g.
#   make docker-image oro=7.0 type=project
#   make docker-image oro=6.1 type=bundle push=false
DOCKER_REPO  ?= algoritmadev/orobox
MEMORY_LIMIT ?= 2048M
oro          ?= 7.0
type         ?= project
push         ?= true

lint:
	@if ! command -v golangci-lint >/dev/null 2>&1; then \
		echo "golangci-lint not found, installing..."; \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(GOPATH)/bin v1.64.5; \
	fi
	golangci-lint run ./...

test:
	go test -v ./...

build:
	go build -o orobox main.go

e2e: build
	E2E_STREAM=1 OROBOX_BIN=$(PWD)/orobox go test -tags e2e -timeout 6h ./e2e/... -v

# docker-image rebuilds and pushes one runtime image locally, the same way the
# docker-build.yml CI workflow does: render the build context with `orobox
# internal-gen-docker`, then `docker build`/`docker push`. The PHP version is taken
# from GetVersionsForOro(oro), so this is how to publish a PHP-version change.
# Rendering runs in a throwaway dir so the repo working tree stays clean.
# Requires `docker login` beforehand when push=true (the default).
#
# This builds no seed database: that bake lives in docker-build.yml, because it installs
# OroCommerce against a throwaway Postgres and takes minutes. An image pushed from here is
# therefore seedless, and every install against it runs oro:install from scratch — fine for
# testing a Dockerfile change, not what should stay on a `-latest` tag. Re-run the CI workflow
# afterwards to put the seed back.
docker-image: build
	@ORO="$(oro)"; TYPE="$(type)"; TAG="$(DOCKER_REPO):$$ORO-$$TYPE-latest"; \
	CTX="$$(mktemp -d)"; trap 'rm -rf "$$CTX"' EXIT; \
	printf 'type: %s\noro_version: "%s"\ndomains:\n  - host: localhost\n' "$$TYPE" "$$ORO" > "$$CTX/.orobox.yaml"; \
	( cd "$$CTX" && CI=1 OROBOX_LOCAL_CONFIG=1 "$(PWD)/orobox" internal-gen-docker ) || { echo "generate build context failed"; exit 1; }; \
	echo "Building $$TAG"; \
	docker build -t "$$TAG" -f "$$CTX/.orobox/Dockerfile" --build-arg MEMORY_LIMIT=$(MEMORY_LIMIT) "$$CTX/.orobox" || exit 1; \
	if [ "$(push)" = "true" ]; then \
		echo "Pushing $$TAG"; \
		docker push "$$TAG" || exit 1; \
	else \
		echo "Skipping push (push=$(push)). Built $$TAG locally."; \
	fi

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