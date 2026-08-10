.PHONY: test build docker-build

test:
	go test ./...

build:
	mkdir -p bin
	go build -o bin/dynasty-analyze ./cmd/analyze
	go build -o bin/ingest-mfl ./cmd/ingest-mfl

docker-build:
	docker build -f Dockerfile -t dynasty-ff-backend:local ..
