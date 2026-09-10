# Every build, lint, test and codegen step runs in a container defined under
# docker/; make and docker are the only host requirements. The images are the
# version pins, so a developer machine and CI run one toolchain, and CI has no
# setup step beyond `make <target>`.
#
# Each recipe is a `docker compose run`, which costs a container start, so
# expensive steps are guarded by stamp files in .stamps/ that re-run only when
# their inputs change. Containers that write into the bind-mounted tree run as
# HOST_UID/HOST_GID so their output is owned by the developer. Each stack has its
# own compose project name and its own published ports, so a test run cannot
# collide with a development stack.
#
# buf is pinned twice: in go.mod for linting and Go generation, and in the client
# lockfile for TypeScript generation, where it has to run beside node.

.DEFAULT_GOAL := help

# make parses .env itself, so it must hold plain KEY=value lines.
include .env
export

.env:
	@touch $@

HOST_UID ?= $(shell id -u)
HOST_GID ?= $(shell id -g)
BUILD_REV ?= $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
export HOST_UID HOST_GID BUILD_REV

COMPOSE_RUN  = docker compose -p stonks      -f docker/docker-compose.yml --env-file .env
COMPOSE_DEV  = docker compose -p stonks-dev  -f docker/docker-compose.yml -f docker/docker-compose.dev.yml --env-file .env
COMPOSE_E2E  = docker compose -p stonks-e2e  -f docker/docker-compose.yml -f docker/docker-compose.e2e.yml --env-file .env
COMPOSE_TEST = docker compose -p stonks-test -f docker/docker-compose.test.yml

# One-shot tool invocations in the dev images, without the rest of the stack.
COMPOSE_TOOLS        = $(COMPOSE_DEV) run --rm --no-deps -T stonks
COMPOSE_TOOLS_ROOT   = $(COMPOSE_DEV) run --rm --no-deps -T -w /app client
COMPOSE_TOOLS_CLIENT = $(COMPOSE_DEV) run --rm --no-deps -T client
COMPOSE_TOOLS_E2E    = $(COMPOSE_DEV) run --rm --no-deps -T -w /app/e2e client
COMPOSE_TESTER       = $(COMPOSE_TEST) run --rm -T tester

# --- Generated code ---

STAMP_DIR     := .stamps
PROTO_FILES   := $(shell find proto -name '*.proto' 2>/dev/null)
QUERY_SQL     := $(shell find server/internal/db/queries -name '*.sql' 2>/dev/null)
MIGRATIONS    := $(shell find server/internal/migrations -name '*.sql' 2>/dev/null)
GENERATE_GO   := $(shell grep -rl --include='*.go' '^//go:generate' server 2>/dev/null)
GENERATE_DEPS := $(PROTO_FILES) $(QUERY_SQL) $(MIGRATIONS) $(GENERATE_GO) \
                 $(wildcard buf.gen.go.yaml buf.gen.ts.yaml buf.gen.e2e.yaml sqlc.yaml go.mod client/package-lock.json)

$(STAMP_DIR):
	@mkdir -p $(STAMP_DIR)

# Each step is skipped when its inputs are absent; buf fails on a module with no
# .proto files rather than doing nothing.
$(STAMP_DIR)/generate: $(GENERATE_DEPS) | $(STAMP_DIR)
ifneq ($(PROTO_FILES),)
	$(COMPOSE_TOOLS) go tool buf generate --template buf.gen.go.yaml
ifneq ($(wildcard client/package-lock.json),)
	$(COMPOSE_TOOLS_ROOT) sh -c 'client/node_modules/.bin/buf generate --template buf.gen.ts.yaml --include-imports && client/node_modules/.bin/buf generate --template buf.gen.e2e.yaml --include-imports'
endif
endif
ifneq ($(wildcard sqlc.yaml),)
	$(COMPOSE_TOOLS) go tool sqlc generate
endif
ifneq ($(GENERATE_GO),)
	$(COMPOSE_TOOLS) go generate ./server/...
endif
	@touch $@

##@ Setup

generate: $(STAMP_DIR)/generate ## Generate protobuf, sqlc and mock code (skipped when current)

##@ Development

run: $(STAMP_DIR)/generate ## Start the dev stack with live reload; app at localhost:8080
	$(COMPOSE_DEV) up -d --build --wait
	@scripts/seed-db.sh "$(COMPOSE_DEV)" "$(DEV_SEED_SQL)"

logs: ## Tail dev stack logs; S=<service> narrows to one
	$(COMPOSE_DEV) logs -f --tail=100 $(S)

stop: ## Stop the dev stack
	$(COMPOSE_DEV) down

build: $(STAMP_DIR)/generate ## Build the server binary to ./stonks
	$(COMPOSE_TOOLS) sh -c 'CGO_ENABLED=0 go build -buildvcs=false -ldflags "-X main.buildRevision=$(BUILD_REV)" -o stonks ./server/cmd/stonks'

##@ Checks

check: fmt-check vet lint client-typecheck e2e-typecheck ## Run every non-test gate

fmt: $(STAMP_DIR)/generate ## Format Go with gofmt and TypeScript with Prettier
	$(COMPOSE_TOOLS) gofmt -w ./server
	$(COMPOSE_TOOLS_CLIENT) node_modules/.bin/prettier --write .
	$(COMPOSE_TOOLS_E2E) node_modules/.bin/prettier --write .

# gofmt -l lists unformatted files and still exits 0.
fmt-check: $(STAMP_DIR)/generate ## Fail if any file is not formatted
	@$(COMPOSE_TOOLS) sh -c 'out=$$(gofmt -l ./server); [ -z "$$out" ] || { echo "$$out"; echo "gofmt: run make fmt"; exit 1; }'
	$(COMPOSE_TOOLS_CLIENT) node_modules/.bin/prettier --check .
	$(COMPOSE_TOOLS_E2E) node_modules/.bin/prettier --check .

vet: $(STAMP_DIR)/generate ## go vet over the server tree
	$(COMPOSE_TOOLS) go vet -tags dbtest ./server/...

lint: lint-go lint-proto lint-ts ## Lint every tree

lint-go: $(STAMP_DIR)/generate ## golangci-lint over the server tree
	$(COMPOSE_TOOLS) go tool golangci-lint run ./server/...

lint-proto: ## buf lint over the proto tree
	$(COMPOSE_TOOLS) go tool buf lint

lint-ts: $(STAMP_DIR)/generate ## ESLint over the client and e2e trees
	$(COMPOSE_TOOLS_CLIENT) npm run lint
	$(COMPOSE_TOOLS_E2E) npm run lint

lint-ts-fix: $(STAMP_DIR)/generate ## ESLint with --fix over the client and e2e trees
	$(COMPOSE_TOOLS_CLIENT) npm run lint:fix
	$(COMPOSE_TOOLS_E2E) npm run lint:fix

client-typecheck: $(STAMP_DIR)/generate ## Typecheck the client
	$(COMPOSE_TOOLS_CLIENT) npm run typecheck

e2e-typecheck: $(STAMP_DIR)/generate ## Typecheck the e2e suite
	$(COMPOSE_TOOLS_E2E) npm run typecheck

##@ Tests

DBTEST_PKGS := $(shell grep -rl --include='*_test.go' '^//go:build dbtest' server 2>/dev/null \
                 | xargs -r -n1 dirname | sort -u | sed 's|^|./|')

test: server-test client-test db-test ## Run the unit, client and integration tests

server-test: $(STAMP_DIR)/generate ## Go unit tests, with recorded HTTP replayed
	$(COMPOSE_TOOLS) go test ./server/...

client-test: $(STAMP_DIR)/generate ## Client unit tests
	$(COMPOSE_TOOLS_CLIENT) npm run test:run

db-test: $(STAMP_DIR)/generate ## Go tests against real Postgres and Redis in the test stack
	@[ -n "$(DBTEST_PKGS)" ] || { echo "db-test: no package carries the dbtest build tag"; exit 1; }
	@rc=0; $(COMPOSE_TESTER) sh -c 'go run ./server/cmd/migrate "$$STONKS_TEST_DATABASE_URL" && go test -tags dbtest -count=1 $(DBTEST_PKGS)' || rc=$$?; $(COMPOSE_TEST) down; exit $$rc

# Recording reaches the real provider, so it needs the network and whatever
# credentials that provider takes, which reach the container through .env. Only
# the named cassette records; every other test replays, so a response that has
# drifted since its own recording cannot rewrite the case built against it.
record: $(STAMP_DIR)/generate ## Re-record one cassette: make record CASSETTE=<name>
	@[ -n "$(CASSETTE)" ] || { echo "usage: make record CASSETTE=<name>"; exit 1; }
	$(COMPOSE_TOOLS) env STONKS_RECORD=$(CASSETTE) go test -count=1 ./server/...

e2e-test: $(STAMP_DIR)/generate ## Playwright against the full stack on shifted ports
	@$(COMPOSE_E2E) --profile test down --remove-orphans 2>/dev/null; \
	rc=0; $(COMPOSE_E2E) up -d --build --wait || rc=$$?; \
	if [ $$rc -eq 0 ]; then $(COMPOSE_E2E) --profile test run --rm playwright || rc=$$?; fi; \
	if [ $$rc -ne 0 ]; then $(COMPOSE_E2E) logs --tail=100 stonks client envoy; fi; \
	$(COMPOSE_E2E) --profile test down --remove-orphans; exit $$rc

##@ Cleanup

clean: ## Remove the binary and the stamps
	rm -f stonks
	rm -rf $(STAMP_DIR)

clean-generated: ## Remove generated code and the generate stamp
	find proto \( -name '*.pb.go' -o -name '*.connect.go' \) -delete 2>/dev/null || true
	find server -name '*_mock.go' -delete 2>/dev/null || true
	rm -rf server/internal/db/gen client/gen e2e/gen
	rm -f $(STAMP_DIR)/generate

clean-docker: ## Remove every stack's containers, images and volumes, including the shared caches
	$(COMPOSE_DEV) down --rmi local --volumes --remove-orphans
	$(COMPOSE_TEST) down --rmi local --volumes --remove-orphans
	$(COMPOSE_E2E) --profile test down --rmi local --volumes --remove-orphans

clean-node: ## Remove node_modules and the Next.js build output
	rm -rf client/node_modules client/.next e2e/node_modules

##@ Help

help: ## Show this help
	@awk 'BEGIN { FS = ":.*## " } \
		/^##@/ { printf "\n%s\n", substr($$0, 5) } \
		/^[a-zA-Z0-9_-]+:.*## / { printf "  %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: generate run logs stop build check fmt fmt-check vet lint lint-go lint-proto lint-ts lint-ts-fix client-typecheck e2e-typecheck test server-test client-test db-test record e2e-test clean clean-generated clean-docker clean-node help
