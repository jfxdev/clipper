.PHONY: build-frontend build test test-race test-integration test-fuzz run dev docker-build audit clean

build-frontend:
	cd frontend && npm ci && npm run build
	rm -rf backend/internal/webembed/dist
	mkdir -p backend/internal/webembed/dist
	cp -r frontend/dist/. backend/internal/webembed/dist/

# Same flags as the Dockerfile's build stage: CGO off is what makes the
# binary static, and therefore what makes the scratch runtime image
# possible. Building differently here would let a cgo dependency creep in
# and only break at image build time.
build: build-frontend
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -buildid=" -o clipper ./cmd/clipper

test:
	cd backend && go test ./...
	cd frontend && npm test -- --run

# The burn-after-read contract is a concurrency guarantee, so the race
# detector is part of the real test run, not an extra.
test-race:
	cd backend && go test -race ./...

test-integration:
	cd backend && go test -tags=integration ./...

# Short fuzz run over the validators that stand between untrusted input and
# the datastore keyspace. CI runs the seed corpus; this is for going deeper
# locally before touching that code.
test-fuzz:
	cd backend && go test -run=XXX -fuzz=FuzzValidateID -fuzztime=30s ./internal/paste/
	cd backend && go test -run=XXX -fuzz=FuzzValidate -fuzztime=30s ./internal/paste/

# Same pinned versions the Security workflow runs, so a local check and CI
# cannot disagree about what "clean" means.
audit:
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...
	cd backend && go run honnef.co/go/tools/cmd/staticcheck@v0.8.1 ./...
	cd frontend && npm audit --audit-level=high

run: build
	./backend/clipper

# frontend/node_modules is rebuilt whenever package-lock.json changes, not
# just when the directory happens to be missing — node_modules/.package-lock.json
# is an npm-managed marker (written by npm ci itself) that tracks which
# lockfile is currently installed.
frontend/node_modules/.package-lock.json: frontend/package-lock.json
	cd frontend && npm ci

# Backend (go run, against docker-compose's redis) and frontend (vite, with
# HMR) side by side. vite.config.ts proxies /api to the backend so the
# browser only ever talks to http://localhost:5173. Copy .env.example to
# .env first to override defaults; unset vars fall back to the same values
# docker-compose uses for redis. .env is sourced only inside the backend's
# own subshell so backend-only secrets (REDIS_PASSWORD, MONGO_URI
# credentials) never leak into the frontend's process env. If either
# process exits, the other is killed and `make dev` exits with the dead
# one's status instead of leaving an orphan running. Ctrl-C stops both.
dev: frontend/node_modules/.package-lock.json
	docker compose up -d redis
	@bash -c '\
		kill_tree() { \
			local pid=$$1; \
			for child in $$(pgrep -P "$$pid" 2>/dev/null); do kill_tree "$$child"; done; \
			kill "$$pid" 2>/dev/null; \
		}; \
		fifo=$$(mktemp -u); mkfifo "$$fifo"; \
		( \
			set -a; [ -f .env ] && source .env; set +a; \
			cd backend && STORE_BACKEND=$${STORE_BACKEND:-redis} \
				REDIS_ADDR=$${REDIS_ADDR:-localhost:6379} \
				REDIS_PASSWORD=$${REDIS_PASSWORD:-devpassword} \
				go run ./cmd/clipper; \
			echo "backend $$?" > "$$fifo" \
		) & backend_pid=$$!; \
		( cd frontend && npm run dev; echo "frontend $$?" > "$$fifo" ) & frontend_pid=$$!; \
		trap "kill_tree $$backend_pid; kill_tree $$frontend_pid" INT TERM; \
		read who status < "$$fifo"; \
		rm -f "$$fifo"; \
		kill_tree $$backend_pid; \
		kill_tree $$frontend_pid; \
		exit "$$status"'

docker-build:
	docker build -t clipper .

clean:
	rm -rf backend/clipper frontend/dist backend/internal/webembed/dist/*
	touch backend/internal/webembed/dist/.gitkeep
