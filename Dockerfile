FROM golang:1.26 AS build
WORKDIR /src/dynasty-ff-backend
COPY dynasty-ff-draft-model /src/dynasty-ff-draft-model
COPY dynasty-ff-backend /src/dynasty-ff-backend
RUN go build -o /out/dynasty-analyze ./cmd/analyze

FROM gcr.io/distroless/base-debian12:nonroot
COPY --from=build /out/dynasty-analyze /usr/local/bin/dynasty-analyze
ENTRYPOINT ["/usr/local/bin/dynasty-analyze"]
