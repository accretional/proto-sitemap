# syntax=docker/dockerfile:1.7
#
# proto-sitemap — the sitemap-svc service image (cmd/sitemap-svc).
#
# proto-sitemap pins its engine dependencies as sibling checkouts
# (`replace github.com/accretional/xmile => ../xmile`, and likewise gluon and
# proto-merge), so a build context of this directory alone cannot see them.
# setup.sh already solves that — it clones all three next to the repo — so the
# builder simply runs it. That is why WORKDIR is /src/proto-sitemap and not
# /src: the siblings must land at /src/xmile, /src/gluon, /src/proto-merge for
# the replace directives to resolve. It also means the build needs network
# access, and that the image tracks each dependency's default branch rather than
# a pinned commit.
#
# The runtime image is static and distroless: the sitemap vocabulary is embedded
# (formats/embed.go) and compiled to a descriptor in-process, so the binary needs
# nothing from disk but the CA bundle distroless already carries.
#
# The service is gRPC; Cloud Run must be deployed with --use-http2 (deploy.sh).

ARG GO_VERSION=1.26

FROM golang:${GO_VERSION} AS build
WORKDIR /src/proto-sitemap

# setup.sh needs git to fetch the sibling modules.
COPY setup.sh ./
RUN git config --global advice.detachedHead false

COPY . .
RUN ./setup.sh
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sitemap-svc ./cmd/sitemap-svc

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sitemap-svc /sitemap-svc
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/sitemap-svc"]
