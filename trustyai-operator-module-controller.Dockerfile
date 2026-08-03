# Build the trustyai-operator-module-controller binary
FROM registry.access.redhat.com/ubi9/go-toolset:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /go/src/github.com/trustyai-explainability/trustyai-operator-module

# Cache module downloads before copying source
COPY trustyai-operator-module/go.mod trustyai-operator-module/go.sum ./
RUN go mod download

# Copy source
COPY trustyai-operator-module/cmd/ cmd/
COPY trustyai-operator-module/pkg/ pkg/

USER root
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    GOFLAGS=-mod=readonly \
    go build -a -o manager ./cmd/trustyai-operator-module

# Bundle manifests template into the image
COPY trustyai-operator-module/hack/get_trustyai_manifests.sh hack/
RUN bash hack/get_trustyai_manifests.sh

# Runtime image
FROM registry.access.redhat.com/ubi9/ubi-minimal:latest
ARG CI_CONTAINER_VERSION=latest

WORKDIR /
COPY --from=builder /go/src/github.com/trustyai-explainability/trustyai-operator-module/manager .
COPY --from=builder /opt/manifests-template /opt/manifests-template

RUN microdnf install -y shadow-utils && \
    useradd -u 1000 -r -g 0 -s /sbin/nologin trustyai && \
    microdnf clean all

USER 1000

ENTRYPOINT ["/manager"]

LABEL com.redhat.component="odh-trustyai-operator-module-container" \
      name="managed-open-data-hub/odh-trustyai-operator-module-rhel9" \
      version="${CI_CONTAINER_VERSION}" \
      summary="TrustyAI Operator Module Controller" \
      description="ODH/RHOAI DSC component module controller for the TrustyAI Operator"
