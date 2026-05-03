FROM node:20-alpine AS web-builder
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ .
RUN npm run build

FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /server ./cmd/server

FROM alpine:latest
RUN apk --no-cache add openssh-client wget && \
  addgroup -S appgroup && \
  adduser -S appuser -G appgroup && \
  mkdir -p /data /app && \
  chown -R appuser:appgroup /data /app

WORKDIR /app
COPY --from=builder /server .
COPY --from=web-builder /web/dist ./web/dist

USER appuser

ENV SHELLWAVE_DATA=/data/shellwave.db

HEALTHCHECK --interval=30s --timeout=3s \
  CMD wget -qO- http://localhost:4000/api/health || exit 1

EXPOSE 4000

CMD ["./server", "-addr", ":4000", "-static", "./web/dist", "-data", "/data/shellwave.db"]