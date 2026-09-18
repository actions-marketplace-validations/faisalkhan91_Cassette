# Multi-stage build → a tiny static image for the cassette CLI.
# Build:  docker build -t cassette .
# Run:    docker run --rm -v "$PWD:/w" -w /w cassette inspect testdata/cassettes/anthropic_text_helper.yaml
FROM golang:1.27 AS build
WORKDIR /src
COPY . .
# Vendored + offline, matching the project's CI guarantees.
ENV GOFLAGS=-mod=vendor GOPROXY=off CGO_ENABLED=0
# Stamped by GoReleaser via --build-arg (see .goreleaser.yaml); the defaults keep a
# plain `docker build` working, just without a release tag.
ARG VERSION=docker
ARG COMMIT=""
ARG DATE=""
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" -o /out/cassette ./cmd/cassette

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/cassette /usr/local/bin/cassette
ENTRYPOINT ["cassette"]
CMD ["--help"]
