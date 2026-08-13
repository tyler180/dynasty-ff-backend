.PHONY: test build docker-build

test:
	go test ./...

build:
	mkdir -p bin
	go build -o bin/dynasty-analyze ./cmd/analyze
	go build -o bin/ingest-mfl ./cmd/ingest-mfl
	go build -o bin/bootstrap ./cmd/lambda

docker-build:
	docker build -f Dockerfile -t dynasty-ff-backend:local ..

tf-plan:
	terraform -chdir=terraform init
	terraform -chdir=terraform plan

tf-apply:
	terraform -chdir=terraform apply -auto-approve
