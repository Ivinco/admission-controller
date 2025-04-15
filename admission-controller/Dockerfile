FROM golang:1.22 as build
WORKDIR /app
COPY go.mod .
COPY go.sum .

RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -o serverd cmd/serverd/main.go

FROM golang:1.22-alpine
COPY --from=build /app/serverd /app/
EXPOSE 8443

CMD ["/app/serverd"]
