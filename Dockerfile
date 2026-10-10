FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /planviewer .

FROM gcr.io/distroless/static
COPY --from=build /planviewer /planviewer
ENV CLAUDE_CONFIG_DIR=/claude
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/planviewer", "serve"]
