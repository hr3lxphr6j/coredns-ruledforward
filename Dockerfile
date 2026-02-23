# Local/dev: build CoreDNS with ruledforward plugin from source, then minimal runtime image.
# CI uses Dockerfile.CI with pre-built binaries from the build job.
FROM golang:1.26-alpine AS builder
RUN apk add --no-cache git perl
WORKDIR /build
RUN git clone --depth 1 --branch v1.14.1 https://github.com/coredns/coredns.git coredns
COPY . /plugin
WORKDIR /build/coredns
RUN perl -i.bak -ne 'print "ruledforward:github.com/hr3lxphr6j/coredns-ruledforward\n" if /^forward:/; print' plugin.cfg && \
    echo '' >> go.mod && echo 'replace github.com/hr3lxphr6j/coredns-ruledforward => /plugin' >> go.mod
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    go generate coredns.go && go get
ARG VERSION=dev
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -ldflags "-s -w -X github.com/coredns/coredns/coremain.GitCommit=${VERSION}" -o /coredns .

FROM alpine:3.19
RUN apk add --no-cache ca-certificates
COPY --from=builder /coredns /usr/bin/coredns
RUN chmod 755 /usr/bin/coredns
EXPOSE 53 53/udp
ENTRYPOINT ["/usr/bin/coredns"]
CMD ["-conf", "/etc/coredns/Corefile"]
