# syntax=docker/dockerfile:1
FROM node:22-alpine AS frontend
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/index.html web/tsconfig.json ./
COPY web/src ./src
RUN npm run build

FROM golang:1.25.7-alpine AS backend
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY db ./db
RUN CGO_ENABLED=0 go build -trimpath -o /out/bifrost ./cmd/bifrost

FROM alpine:3.23 AS runtime
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 bifrost \
    && adduser -S -D -H -u 10001 -G bifrost bifrost
WORKDIR /app
COPY --from=backend --chown=10001:10001 /out/bifrost /app/bifrost
COPY --from=frontend --chown=10001:10001 /build/web/dist /app/web/dist
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/app/bifrost"]
CMD ["-mode", "readonly", "-port", "8080"]
