.PHONY: build test vet lint docker-build clean

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o tailgrant-server ./cmd/tailgrant-server
	CGO_ENABLED=0 go build -ldflags="-s -w" -o tailgrant-worker ./cmd/tailgrant-worker

test:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

docker-build:
	docker buildx build --platform linux/amd64 -t tailgrant:local .

clean:
	rm -f tailgrant-server tailgrant-worker
