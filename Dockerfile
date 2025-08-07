FROM golang:1.24.6-alpine AS builder

WORKDIR /app

COPY server/* ./
RUN CGO_ENABLED=0 GOOS=linux go build -o ws-server .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

COPY --from=builder /app/ws-server .

EXPOSE 8080

CMD ["./ws-server"]
