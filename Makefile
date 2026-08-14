.PHONY: dev-api dev-user dev-admin dev-up migrate compose-migrate backup-postgres backup-postgres-native restore-postgres restore-postgres-native native-prepare-storage native-rotate-logs native-status native-start native-stop native-restart local-status local-start local-stop local-restart build test fmt verify-toolchain

GO_ENV = CGO_ENABLED=0 GOCACHE=$(CURDIR)/api/.cache/go-build GOMODCACHE=$(CURDIR)/api/.cache/go-mod
COMPOSE = docker compose --env-file .env -f docker-compose.yml

dev-up:
	docker compose --env-file .env -f docker-compose.yml -f docker-compose.dev.yml up --build

dev-api:
	cd api && $(GO_ENV) go run ./cmd/linlinqi api

dev-user:
	cd user && npm run dev

dev-admin:
	cd admin && npm run dev

migrate:
	cd api && $(GO_ENV) BOOTSTRAP_ADMIN=false SEED_DATA=false go run ./cmd/linlinqi migrate

compose-migrate:
	$(COMPOSE) run --rm migrate

backup-postgres:
	./scripts/backup-postgres.sh

backup-postgres-native:
	./scripts/backup-postgres-native.sh

native-prepare-storage:
	./scripts/prepare-native-storage.sh

native-rotate-logs:
	./scripts/rotate-native-macos-logs.sh

native-status native-start native-stop native-restart:
	./scripts/native-macos-service.sh $(patsubst native-%,%,$@)

local-status local-start local-stop local-restart:
	./scripts/project-local-service.sh $(patsubst local-%,%,$@)

restore-postgres:
	@test -n "$(BACKUP)" || (printf '%s\n' 'BACKUP=/absolute/path/to/backup.dump is required' >&2; exit 2)
	./scripts/restore-postgres.sh "$(BACKUP)"

restore-postgres-native:
	@test -n "$(BACKUP)" || (printf '%s\n' 'BACKUP=/absolute/path/to/backup.dump is required' >&2; exit 2)
	./scripts/restore-postgres-native.sh "$(BACKUP)"

verify-toolchain:
	node scripts/verify-toolchain.mjs

build: verify-toolchain
	cd api && $(GO_ENV) go build -trimpath -o ./linlinqi ./cmd/linlinqi
	cd user && npm run build
	cd admin && npm run build

test: verify-toolchain
	cd api && $(GO_ENV) go test ./... && $(GO_ENV) go vet ./...

fmt:
	cd api && find . -type f -name '*.go' -print0 | xargs -0 gofmt -w
	cd user && npx prettier --write src
	cd admin && npx prettier --write src
