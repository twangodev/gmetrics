# syntax=docker/dockerfile:1
# Multi-stage build for the gmetrics GitHub Action.
# Final image is the distroless static-debian12:nonroot base (~2MB) plus the
# statically-linked gmetrics binary — orders of magnitude smaller than the
# upstream lowlighter/metrics image (which bundles Chrome).

FROM golang:1.26-alpine AS builder
WORKDIR /src

# Cache module downloads in a separate layer.
COPY go.mod go.sum ./
RUN go mod download

# Bring in the rest of the source and build a static, stripped binary.
COPY . .
RUN CGO_ENABLED=0 GOFLAGS="-trimpath" \
    go build -ldflags="-s -w" -o /out/gmetrics ./cmd/gmetrics

# Final stage: alpine (smallest fully-static image that still provides a POSIX
# /bin/sh, required by entrypoint.sh). The original spec called for
# gcr.io/distroless/static-debian12:nonroot, but distroless has no shell, so
# the #!/bin/sh entrypoint cannot execute there. Alpine keeps the final image
# small (~10MB base) while supporting the shell entrypoint contract.
FROM alpine:3.23
RUN apk add --no-cache ca-certificates git
COPY --from=builder /out/gmetrics /usr/local/bin/gmetrics
COPY exclusion.toml /usr/local/share/gmetrics/exclusion.toml
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh
ENTRYPOINT ["/entrypoint.sh"]
