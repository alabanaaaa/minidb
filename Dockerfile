FROM golang:1.25.6-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o pos ./cmd/pos
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o admin ./cmd/admin

FROM alpine:3.19

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /app/pos /app/server /app/admin ./
COPY --from=builder /app/migrations ./migrations

RUN mkdir -p /app/data

EXPOSE 8080

CMD ["./server"]
