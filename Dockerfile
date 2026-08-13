FROM golang:1.26 AS build
WORKDIR /src/dynasty-ff-backend
# Copy go modules first to leverage layer caching
COPY dynasty-ff-backend/go.mod dynasty-ff-backend/go.sum ./
COPY dynasty-ff-models /src/dynasty-ff-models
RUN go mod edit -replace github.com/tyler180/dynasty-ff-models=/src/dynasty-ff-models \
    && go mod download

COPY mfl/mfl-mcp/go.mod mfl/mfl-mcp/go.sum /src/mfl-mcp/
RUN cd /src/mfl-mcp && go mod download

# Copy source
COPY dynasty-ff-backend/cmd ./cmd
COPY dynasty-ff-backend/config ./config
COPY dynasty-ff-backend/data ./data
COPY dynasty-ff-backend/docs ./docs
COPY dynasty-ff-backend/internal ./internal
COPY mfl/mfl-mcp/cmd /src/mfl-mcp/cmd
COPY mfl/mfl-mcp/internal /src/mfl-mcp/internal

# Build the Lambda custom runtime bootstrap with minimal symbol info.
ENV CGO_ENABLED=0
RUN GOOS=linux GOARCH=amd64 go build -tags lambda.norpc -trimpath -ldflags "-s -w" -o /out/bootstrap ./cmd/lambda
RUN cd /src/mfl-mcp && GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o /out/mfl-mcp ./cmd/mfl-mcp

FROM public.ecr.aws/lambda/provided:al2023
COPY --from=build /out/bootstrap ./bootstrap
COPY --from=build /out/mfl-mcp ./mfl-mcp
COPY --from=build /src/dynasty-ff-backend/config ./config
ENTRYPOINT ["./bootstrap"]
