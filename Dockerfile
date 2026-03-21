# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Install build dependencies
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/node ./cmd/node

# Runtime stage
FROM alpine:3.19

# Create non-root user
RUN addgroup -S app && adduser -S app -G app

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk add --no-cache ca-certificates

# Copy binary from builder
COPY --from=builder /app/node .

# Set ownership and switch to non-root user
RUN chown -R app:app /app
USER app

ENTRYPOINT ["./node"]
