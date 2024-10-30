FROM quay.io/projectquay/golang:1.22 as builder

ENV GOPATH /go
ENV GO111MODULE on
ENV GOPROXY https://goproxy.cn,direct

WORKDIR /go/src/dcloud-dhcp-controller

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

COPY main.go main.go
COPY pkg/  pkg/

RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} go build -o dhcp-controller .

FROM quay.io/jitesoft/alpine:3.20

RUN adduser -S -D -H -h /app dhcp-controller

USER dhcp-controller

COPY --from=builder /go/src/dcloud-dhcp-controller/dhcp-controller /app/

WORKDIR /app

ENTRYPOINT ["./dhcp-controller"]