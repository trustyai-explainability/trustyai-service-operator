# Build the trustyai-operator-module-controller binary.
# Build context must be the repository root (trustyai-service-operator/) so that
# both the module source (trustyai-operator-module/) and the workload operator
# manifests (config/) are accessible during the build.
FROM registry.access.redhat.com/ubi9/go-toolset:1.26 AS builder
ARG TARGETOS
ARG TARGETARCH

ARG VERSION=unknown

WORKDIR /go/src/github.com/trustyai-explainability/trustyai-operator-module

# Cache module downloads before copying source
COPY trustyai-operator-module/go.mod trustyai-operator-module/go.sum ./
RUN go mod download

# Copy module source
COPY trustyai-operator-module/cmd/ cmd/
COPY trustyai-operator-module/pkg/ pkg/

USER root
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    GOFLAGS=-mod=readonly \
    go build -a \
    -ldflags "-X github.com/trustyai-explainability/trustyai-operator-module/pkg/trustyaimodule.Version=${VERSION}" \
    -o manager ./cmd/trustyai-operator-module

# Bundle workload operator manifests into the image.
# config/ is the workload operator's kustomize tree; get_trustyai_manifests.sh
# copies it into /opt/manifests-template/trustyai-operator/config/ so the
# module controller can render the real overlays at runtime.
COPY trustyai-operator-module/hack/get_trustyai_manifests.sh hack/
COPY config/ workload-config/
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
