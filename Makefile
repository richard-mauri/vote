 VERSION=v0.0.1
 TAGDESC="First release"
 BUILDTIME?=$$(date +%m-%d-%Y-%H:%M)
 VERSIONSTRING=${VERSION}-${BUILDTIME}
 GOFMT_FILES?=$$(find . -name '*.go')
 export GO111MODULE=on

default: build

all: fmt build test clean run

build:
	go build -o vote -ldflags "-X main.VersionString=${VERSION}-${BUILDTIME}"  .
	CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o vote.linux -ldflags "-X main.VersionString=${VERSION}-${BUILDTIME}"
	docker build -f Dockerfile.scratch -t vote:latest .

test:
	go test github.com/richard-mauri/vote

fmt:
	gofmt -w $(GOFMT_FILES)

release:
	git tag -a ${VERSION} -m ${TAGDESC}
	RELVERSION=${VERSIONSTRING} goreleaser

run:
	docker-compose up

clean:
	rm -rf data
	docker rmi -f vote

.PHONY: all build default test fmt release run clean
