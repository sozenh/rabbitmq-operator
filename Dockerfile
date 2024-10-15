# Build the manager binary
FROM --platform=$BUILDPLATFORM registry.woqutech.com/library/golang:1.22 AS builder

WORKDIR /workspace

# Copy the go source
COPY . .

# Build
ARG TARGETOS
ARG TARGETARCH
ENV GOOS=$TARGETOS
ENV GOARCH=$TARGETARCH
RUN CGO_ENABLED=0 GO111MODULE=on go build -a -tags timetzdata -o manager main.go

# ---------------------------------------
FROM registry.woqutech.com/google_containers/alpine:3.13 AS etc-builder

RUN echo "rabbitmq-cluster-operator:x:1000:" > /etc/group && \
    echo "rabbitmq-cluster-operator:x:1000:1000::/home/rabbitmq-cluster-operator:/usr/sbin/nologin" > /etc/passwd

RUN apk add -U --no-cache ca-certificates

# ---------------------------------------
FROM scratch

ARG GIT_COMMIT
LABEL GitCommit=$GIT_COMMIT

WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=etc-builder /etc/passwd /etc/group /etc/
COPY --from=etc-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

USER 1000:1000

ENTRYPOINT ["/manager"]
