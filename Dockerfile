# syntax=docker/dockerfile:1.7

# Codegen toolchain pinned to match the Makefile so a `docker build` produces
# identical generated output to a local `make build`.
ARG GO_VERSION=1.24
ARG BUF_VERSION=v1.55.0
ARG PROTOC_GEN_GO_VERSION=v1.36.0
ARG PROTOC_GEN_GRPC_VERSION=v1.5.1

FROM golang:${GO_VERSION} AS builder
ARG BUF_VERSION
ARG PROTOC_GEN_GO_VERSION
ARG PROTOC_GEN_GRPC_VERSION

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates make git \
 && rm -rf /var/lib/apt/lists/* \
 && update-ca-certificates

# Install pinned codegen tools (architecture rule 9: never `@latest`).
RUN go install github.com/bufbuild/buf/cmd/buf@${BUF_VERSION} \
 && go install google.golang.org/protobuf/cmd/protoc-gen-go@${PROTOC_GEN_GO_VERSION} \
 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@${PROTOC_GEN_GRPC_VERSION}

ENV PATH=/go/bin:${PATH}

WORKDIR /src

# Cache go module downloads independently of the rest of the source.
COPY go.mod go.sum ./
RUN go mod download

# Copy the source tree (including .git so ldflags can stamp the build).
COPY . ./

# proto-gen runs as part of `make build`.
RUN make build

# --- Runtime stage -----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime

# CA bundle for outbound TLS calls (gRPC clients dial upstreams).
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /src/evm-oracle-demo-api /evm-oracle-demo-api

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/evm-oracle-demo-api"]
CMD ["serve"]
