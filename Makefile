# Set APP to the name of the application
APP:=evm-oracle-demo-api

# Set APP_ENTRY_POINT to the main Go file for the application
APP_ENTRY_POINT:=cmd/evm-oracle-demo-api.go

# Set BUILD_OUT_DIR to the directory where the built executable should be placed
BUILD_OUT_DIR:=./

# path to version package
GITVER_PKG:=github.com/asolovov/evm-oracle-demo-api/pkg/version

# Set GOOS and GOARCH to the current system values using the go env command
GOOS=$(shell go env GOOS)
GOARCH=$(shell go env GOARCH)

# set git related vars for versioning (tolerate repos without tags)
TAG 		:= $(shell git describe --abbrev=0 --tags 2>/dev/null || true)
COMMIT		:= $(shell git rev-parse HEAD)
BRANCH		?= $(shell git rev-parse --abbrev-ref HEAD)
REMOTE		:= $(shell git config --get remote.origin.url)
BUILD_DATE	:= $(shell date +'%Y-%m-%dT%H:%M:%SZ%Z')

# Set RELEASE to either the current TAG or COMMIT
RELEASE :=
ifeq ($(TAG),)
	RELEASE := $(COMMIT)
else
	RELEASE := $(TAG)
endif

# append versioner vars to ldflags
LDFLAGS += -X $(GITVER_PKG).ServiceName=$(APP)
LDFLAGS += -X $(GITVER_PKG).CommitTag=$(TAG)
LDFLAGS += -X $(GITVER_PKG).CommitSHA=$(COMMIT)
LDFLAGS += -X $(GITVER_PKG).CommitBranch=$(BRANCH)
LDFLAGS += -X $(GITVER_PKG).OriginURL=$(REMOTE)
LDFLAGS += -X $(GITVER_PKG).BuildDate=$(BUILD_DATE)
LDFLAGS += -X $(GITVER_PKG).Release=$(RELEASE)

.PHONY: all tidy update run build test test-coverage clean lint lint-install \
        proto-update compose-up compose-down compose-restart rename

all: tidy build test

tidy:
	go mod tidy

update:
	go get -u ./...

# Build the application binary. Codegen is wired in a later commit as a prerequisite.
build:
	env CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags="-w -s ${LDFLAGS}" -o $(BUILD_OUT_DIR)/$(APP) $(APP_ENTRY_POINT)

run:
	go run -race $(APP_ENTRY_POINT) serve

test:
	go test ./...

test-coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

clean:
	rm -f $(BUILD_OUT_DIR)/$(APP)

lint:
	golangci-lint run ./...

lint-install:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)

# --- Protocols subtree -------------------------------------------------------
# The protocols/ directory is wired in via `git subtree add` against
# https://github.com/asolovov/evm-oracle-demo-protocols.git. This target lets a
# maintainer pull the latest IDL into the local checkout; the agent is not
# permitted to run it without confirmation (it touches a remote).
PROTO_DIR := ./protocols
PROTO_REPO ?= https://github.com/asolovov/evm-oracle-demo-protocols.git
PROTO_BRANCH ?= main

proto-update:
	@echo "Updating protobuf subtree from $(PROTO_REPO)..."
	@git subtree pull --prefix=$(PROTO_DIR) $(PROTO_REPO) $(PROTO_BRANCH) --squash
	@echo "Subtree updated successfully"

# --- Local stack helpers (Redis only — this BFF has no relational DB) --------
compose-up:
	docker compose up -d

compose-down:
	docker compose down

compose-restart:
	docker compose down
	docker compose up -d

# --- Project rename ----------------------------------------------------------
rename:
ifndef NEW_NAME
	@echo "Error: NEW_NAME parameter is required"
	@echo "Usage: make rename NEW_NAME=my-new-service"
	@exit 1
endif
	@bash scripts/rename.sh "$(NEW_NAME)"
