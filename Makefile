BINARY    := pincer
VERSION   := 0.1.0
LDFLAGS   := -s -w -X github.com/igorsilveira/pincer/cmd/pincer.version=$(VERSION)
IMAGE     := pincer
PORT      := 18789

.PHONY: all build run test vet lint fmt clean docker docker-run install

all: vet test build

build:
	go build -mod=vendor -ldflags="$(LDFLAGS)" -trimpath -o $(BINARY) .

run: build
	./$(BINARY) start

test:
	go test -mod=vendor ./...

vet:
	go vet -mod=vendor ./...

lint:
	golangci-lint run ./...

fmt:
	gofmt -w -s .

clean:
	rm -f $(BINARY)
	go clean -cache -testcache

docker:
	docker build -t $(IMAGE):$(VERSION) -t $(IMAGE):latest .

docker-run:
	docker run -d -p $(PORT):$(PORT) -v pincer-data:/data $(IMAGE):latest

install: build
	cp $(BINARY) $(GOPATH)/bin/$(BINARY)
