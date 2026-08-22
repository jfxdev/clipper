.PHONY: build-frontend build test test-race test-integration test-fuzz run docker-build audit clean

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

audit:
	cd backend && go run golang.org/x/vuln/cmd/govulncheck@latest ./...
	cd frontend && npm audit --audit-level=high

run: build
	./backend/clipper

docker-build:
	docker build -t clipper .

clean:
	rm -rf backend/clipper frontend/dist backend/internal/webembed/dist/*
	touch backend/internal/webembed/dist/.gitkeep
