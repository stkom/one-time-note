FROM golang:1.26.5-alpine@sha256:0178a641fbb4858c5f1b48e34bdaabe0350a330a1b1149aabd498d0699ff5fb2 AS build
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
