FROM golang:1.26.4-alpine@sha256:3ad57304ad93bbec8548a0437ad9e06a455660655d9af011d58b993f6f615648 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -o /out/one-time-note . \
    && mkdir -p /out/data

FROM scratch
ENV NOTE_HOST=0.0.0.0
ENV NOTE_PORT=8080
ENV NOTE_DB_PATH=/data/data.db
VOLUME ["/data"]
EXPOSE 8080
USER 65532:65532
COPY --from=build --chown=65532:65532 /out/data /data
COPY --from=build /out/one-time-note /one-time-note
ENTRYPOINT ["/one-time-note"]
