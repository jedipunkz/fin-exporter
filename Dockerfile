FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /fin-exporter .

# distroless/static includes CA certificates and runs as uid 65532.
FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /fin-exporter /fin-exporter
ENTRYPOINT ["/fin-exporter", "-config", "/etc/fin-exporter/fin-exporter.yaml"]
