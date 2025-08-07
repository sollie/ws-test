FROM golang:1.24.6-alpine AS builder

WORKDIR /app

COPY source/* ./
RUN CGO_ENABLED=0 GOOS=linux go build -o ws-test-service .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/ws-test-service .

EXPOSE 8080

CMD ["./ws-test-service"]
