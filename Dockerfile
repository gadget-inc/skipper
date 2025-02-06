# syntax=docker/dockerfile:1

FROM golang:1.23 AS build
WORKDIR /build
RUN go env -w GOMODCACHE=/go/pkg/mod
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -o /out/fusion .

FROM ubuntu:latest
RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl unzip && \
    apt-get autoremove -y && \
    apt-get purge -y --auto-remove && \
    rm -rf /var/lib/apt/lists/*
RUN update-ca-certificates
RUN useradd -ms /bin/bash fusion
WORKDIR /home/fusion
USER fusion
COPY --from=build --chown=fusion /out/fusion .
ENTRYPOINT ["./fusion"]
