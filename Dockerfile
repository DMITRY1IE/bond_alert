FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /server ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata postgresql15-client
WORKDIR /app
COPY --from=build /server /server
COPY migrations /app/migrations
COPY scripts/docker-entrypoint.sh /docker-entrypoint.sh
RUN chmod +x /docker-entrypoint.sh && sed -i 's/\r$//' /docker-entrypoint.sh 2>/dev/null || true
ENV TZ=Europe/Moscow
EXPOSE 8000
ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["/server"]
