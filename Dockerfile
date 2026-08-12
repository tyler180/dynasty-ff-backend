FROM golang:1.26 AS build
WORKDIR /src/dynasty-ff-backend
# Copy go modules first to leverage layer caching
COPY dynasty-ff-backend/go.mod dynasty-ff-backend/go.sum ./
COPY dynasty-ff-models /src/dynasty-ff-models
RUN go mod edit -replace github.com/tyler180/dynasty-ff-models=/src/dynasty-ff-models \
    && go mod download

# Copy source
COPY dynasty-ff-backend/cmd ./cmd
COPY dynasty-ff-backend/config ./config
COPY dynasty-ff-backend/data ./data
COPY dynasty-ff-backend/docs ./docs
COPY dynasty-ff-backend/internal ./internal

# Build a statically linked binary with minimal symbol info
ENV CGO_ENABLED=0
RUN GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/dynasty-analyze ./cmd/analyze

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dynasty-analyze /usr/local/bin/dynasty-analyze
ENTRYPOINT ["/usr/local/bin/dynasty-analyze"]
