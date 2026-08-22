# syntax=docker/dockerfile:1

# Base images are pinned by digest, not just by tag: a tag is mutable, so
# "node:22-alpine" can point at different content tomorrow and a build that
# passed review is not the build that ships. Renovate/Dependabot updates
# these lines the same way it updates any other dependency.
FROM node:26-alpine@sha256:aadf416b2cdce311a8811ba3f0608a61b77dbf997500e2eafe781b51f6a0b019 AS frontend-build
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Go 1.24 is out of support, so its standard library keeps advisories that
# will never be fixed on that line and end up compiled into the binary. The
# builder tracks a supported release; backend/go.mod pins the same toolchain
# so a local `make build` cannot produce a weaker binary than the image.
FROM golang:1.27-alpine@sha256:4c9fe60190a2a3350ddc51de80d0224b8a6698d12bdfc999fee45ea9d6c46dbc AS backend-build
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend-build /src/frontend/dist ./internal/webembed/dist
# CGO off gives a fully static binary (verified: `file` reports "statically
# linked", ldd reports "not a dynamic executable"), which is what makes the
# scratch stage below possible. -trimpath strips local build paths out of
# the binary; -s -w -buildid= drop the symbol table, DWARF data and build
# id, shrinking what a person poking at a compromised container gets for
# free and making the build byte-reproducible.
RUN CGO_ENABLED=0 GOFLAGS=-mod=readonly \
    go build -trimpath -ldflags="-s -w -buildid=" -o /clipper ./cmd/clipper

# The runtime image is scratch: no shell, no package manager, no busybox, no
# libc. There is nothing in it to pivot to if the process is compromised,
# and nothing in it for a CVE scanner to find, because the only file is the
# binary itself. This works because the frontend is embedded in the binary
# via go:embed, so there are no static assets to mount either.
#
# Two things still have to be copied in:
#   * the CA bundle, because Go's crypto/x509 has no built-in roots on
#     Linux — without it every TLS connection (DynamoDB over HTTPS, Redis
#     with REDIS_TLS, MongoDB with tls=true) fails to verify;
#   * nothing else. /etc/resolv.conf, /etc/hosts and /etc/hostname are bind
#     mounted by the container runtime, so DNS works with the pure-Go
#     resolver that CGO_ENABLED=0 selects.
FROM scratch
COPY --from=backend-build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=backend-build /clipper /usr/local/bin/clipper

# Numeric uid:gid rather than a name: scratch has no /etc/passwd for a name
# to resolve against. 10001 is outside the range any distro assigns.
USER 10001:10001

EXPOSE 8080

# Probing from inside the binary, since scratch has no curl or wget.
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD ["/usr/local/bin/clipper", "-healthcheck"]

ENTRYPOINT ["/usr/local/bin/clipper"]
