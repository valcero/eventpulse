# syntax=docker/dockerfile:1.7
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ENV CGO_ENABLED=0
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    for b in api worker receiver; do \
      go build -trimpath -ldflags="-s -w" -o /out/$b ./cmd/$b || exit 1; \
    done

# One image, three binaries: run with `/app/api`, `/app/worker` or `/app/receiver`.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/app/api"]
