.PHONY: dev-server dev-web build docker test check clean

dev-server:
	go run ./cmd/server

dev-web:
	cd web && npm run dev

build:
	cd web && npm ci && npm run build
	go build -o bin/server ./cmd/server

docker:
	docker build -t shellwave:local .

test:
	go test ./...
	cd web && npm ci && npm run check && npm run build

check:
	go vet ./...
	go test ./...
	cd web && npm ci && npm run check && npm run build
	docker build -t shellwave:test .

clean:
	rm -rf bin/
	rm -f server
	rm -rf web/dist/
	find . -name .DS_Store -delete