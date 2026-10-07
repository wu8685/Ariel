ARG WEB_BUILD_IMAGE=golang:1.26-alpine
FROM ${WEB_BUILD_IMAGE} AS web-build
RUN --mount=type=cache,target=/var/cache/apk,sharing=locked \
    if ! command -v node >/dev/null || ! command -v npm >/dev/null; then apk add --cache-dir /var/cache/apk --update-cache nodejs npm; fi \
    && node --version \
    && npm --version
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm,sharing=locked npm ci
COPY protocol/v1.schema.json /src/protocol/v1.schema.json
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS relay-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/relay/ ./cmd/relay/
COPY internal/relay/ ./internal/relay/
COPY protocol/ ./protocol/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ariel-relay ./cmd/relay

FROM alpine:3.23 AS runtime
RUN addgroup -S -g 10001 ariel \
    && adduser -S -D -H -u 10001 -G ariel ariel
WORKDIR /app
RUN mkdir -p /app/web/dist
COPY --from=relay-build /out/ariel-relay /app/ariel-relay
COPY --from=web-build /src/web/dist/ /app/web/dist/

ENV ARIEL_LISTEN=0.0.0.0:8080 \
    ARIEL_WEB_DIST=/app/web/dist

EXPOSE 8080
USER ariel
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O - http://127.0.0.1:8080/healthz | grep -qx ok
ENTRYPOINT ["/app/ariel-relay"]
