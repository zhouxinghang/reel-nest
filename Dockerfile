FROM node:22-alpine AS frontend
WORKDIR /src
COPY package.json package-lock.json ./
RUN npm ci --no-fund --no-audit
COPY web ./web
RUN npm run build:web

FROM golang:1.25-alpine AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY api ./api
COPY cmd ./cmd
COPY internal ./internal
COPY --from=frontend /src/internal/web/dist ./internal/web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /reelnest ./cmd/reelnest

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -g 10001 reelnest && adduser -D -u 10001 -G reelnest reelnest && mkdir /data && chown reelnest:reelnest /data
COPY --from=backend /reelnest /usr/local/bin/reelnest
USER 10001:10001
ENV DATA_DIR=/data LISTEN_ADDR=:8090
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s CMD wget -q --spider http://127.0.0.1:8090/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/reelnest"]
