FROM golang:1.27.2-alpine@sha256:f92b6ef800e499660581efdabdf25d9d817a9d124eaf900924f0504e7e27e12d AS build
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
