# One image per service: build with --build-arg SERVICE=hub|manager|worker|client.
# Build context is the repository root:
#   docker build -f examples/langgraph/independent_engineering_team/Dockerfile --build-arg SERVICE=worker -t team-worker .
FROM golang:1.27 AS build
ARG SERVICE=worker
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/service ./examples/langgraph/independent_engineering_team/cmd/${SERVICE}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/service /service
ENTRYPOINT ["/service"]
