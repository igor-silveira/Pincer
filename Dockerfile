FROM golang:1.24-alpine AS build

WORKDIR /src
COPY . .

ARG VERSION=0.1.0
RUN go build -mod=vendor -ldflags="-s -w -X github.com/igorsilveira/pincer/cmd/pincer.version=${VERSION}" -trimpath -o /pincer .

FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S pincer \
    && adduser -S -G pincer -h /home/pincer pincer \
    && mkdir -p /data \
    && chown pincer:pincer /data

COPY --from=build /pincer /usr/local/bin/pincer

USER pincer

ENV PINCER_DATA_DIR=/data

EXPOSE 18789

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:18789/healthz || exit 1

VOLUME ["/data"]

ENTRYPOINT ["pincer"]
CMD ["start", "--config", "/data/pincer.toml"]
