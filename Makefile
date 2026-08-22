.PHONY: build-frontend build test test-integration run docker-build clean

build-frontend:
	cd frontend && npm ci && npm run build
	rm -rf backend/internal/webembed/dist
	mkdir -p backend/internal/webembed/dist
	cp -r frontend/dist/. backend/internal/webembed/dist/

build: build-frontend
	cd backend && go build -o clipper ./cmd/clipper

test:
	cd backend && go test ./...
	cd frontend && npm test -- --run

test-integration:
	cd backend && go test -tags=integration ./...

run: build
	./backend/clipper

docker-build:
	docker build -t clipper .

clean:
	rm -rf backend/clipper frontend/dist backend/internal/webembed/dist/*
	touch backend/internal/webembed/dist/.gitkeep
