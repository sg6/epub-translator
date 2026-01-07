# Build
FROM golang:1.24-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./

RUN go mod download

COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -o epub-translator main.go


# Run
FROM alpine:latest

RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the binary from the builder stage to a system path
COPY --from=builder /app/epub-translator /usr/local/bin/epub-translator

# Set entrypoint to absolute path
ENTRYPOINT ["/usr/local/bin/epub-translator"]
