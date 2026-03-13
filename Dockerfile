# Build stage
FROM golang:1.25.6-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /app

# Copy go mod files
COPY go.mod ./

# Copy go.sum if it exists
COPY go.sum* ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the POS CLI binary
RUN CGO_ENABLED=0 GOOS=linux go build -o pos ./cmd/pos

# Build the server binary
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# Runtime stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Copy binaries from builder
COPY --from=builder /app/pos /app/server ./

# Create data directory for persistence
RUN mkdir -p /app/data

# Expose API server port
EXPOSE 8080

# Default command runs the server
CMD ["./server"]