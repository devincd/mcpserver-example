WHAT ?= mcpserver

ldflags = "\
	-X devincd.io/mcpserver-example/pkg/cmd.buildstamp=`date -u '+%Y-%m-%d_%I:%M:%S%p'` \
	-X devincd.io/mcpserver-example/pkg/cmd.buildGitRevision=`git rev-parse HEAD` \
"

all: clean build

.PHONY: clean
clean:
	rm -f _output/${WHAT}

.PHONY: build
build:
	CGO_ENABLED=0 go build -ldflags=${ldflags} -o _output/${WHAT} main.go
