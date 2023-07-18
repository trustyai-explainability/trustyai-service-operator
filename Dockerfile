# Build the manager binary
FROM registry.access.redhat.com/ubi8/go-toolset:1.19 as builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/

# Build
# the GOARCH has not a default value to allow the binary be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
USER root
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM registry.access.redhat.com/ubi8/ubi-minimal:8.7
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]

LABEL com.redhat.component="odh-trustyai-service-operator-container" \
      name="managed-open-data-hub/odh-trustyai-service-operator-rhel8" \
      version="${CI_CONTAINER_VERSION}" \
      summary="odh-trustyai-service-operator" \
      io.openshift.expose-services="" \
      io.k8s.display-name="odh-trustyai-service-operator" \
      maintainer="['managed-open-data-hub@redhat.com']" \
      description="The TrustyAI Operator manages TrustyAI deployments within a k8s cluster" \
      com.redhat.license_terms="https://www.redhat.com/licenses/Red_Hat_Standard_EULA_20191108.pdf"
