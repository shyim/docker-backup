# CSS build stage
FROM --platform=$BUILDPLATFORM node:24-alpine AS css-builder

WORKDIR /build

COPY package.json package-lock.json ./
RUN npm ci

COPY tailwind.config.js ./
COPY internal/dashboard/static/src ./internal/dashboard/static/src
COPY internal/dashboard/templates ./internal/dashboard/templates

RUN npm run build:css

# Go build stage
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git ca-certificates

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Copy built CSS from css-builder
COPY --from=css-builder /build/internal/dashboard/static/app.css ./internal/dashboard/static/app.css

# Build with cross-compilation support
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-w -s" -o docker-backup ./cmd/docker-backup

# Runtime stage
FROM alpine

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

COPY --from=builder /build/docker-backup /usr/local/bin/docker-backup

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/docker-backup"]
CMD ["daemon"]
