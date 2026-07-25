FROM --platform=${BUILDPLATFORM} golang:1.24-alpine AS build

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags="-w -s -X main.Version=${VERSION}" \
    -o /azghost ./cmd/azghost

FROM alpine:3.20

RUN apk add --no-cache ca-certificates
WORKDIR /app

COPY --from=build /azghost /app/azghost
COPY scripts /app/scripts
COPY scenarios /app/scenarios

EXPOSE 8888

ENTRYPOINT ["/app/azghost"]
CMD ["node", "--listen", ":8888"]
