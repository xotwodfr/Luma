# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/luma-server ./cmd/luma-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/luma-server /luma-server
USER nonroot:nonroot
ENV LUMA_LISTEN=:8790 LUMA_UPLOAD_DIR=/data/uploads
EXPOSE 8790
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s CMD ["/luma-server", "healthcheck"]
ENTRYPOINT ["/luma-server"]
