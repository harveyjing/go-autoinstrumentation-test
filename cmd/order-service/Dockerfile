FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -o /app/server .

FROM gcr.io/distroless/static
COPY --from=build /app/server /app/server
EXPOSE 8080
ENTRYPOINT ["/app/server"]
