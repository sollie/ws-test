# WebSocket Test Service

A simple Go service that provides WebSocket connections and reports server identification and client statistics.

## Features

- **WebSocket Endpoint** (`/ws`): Accepts WebSocket connections and sends server identification
- **HTTP Status Endpoint** (`/`): Returns JSON with server ID and connected client count
- **Kubernetes Support**: Automatically detects pod name when running in Kubernetes
- **Client Tracking**: Thread-safe tracking of concurrent WebSocket connections

## Server Identification

The service identifies itself using the first available option:
1. `POD_NAME` environment variable (set by Kubernetes)
2. `HOSTNAME` environment variable
3. System hostname
4. Fallback to "unknown-server"

## API

### WebSocket (`/ws`)
Connects via WebSocket and receives JSON messages:
```json
{
  "type": "welcome|status",
  "server_id": "pod-name-12345",
  "timestamp": "2025-08-07T10:30:00Z",
  "clients": 3
}
```

### HTTP Status (`/`)
Returns current server status:
```json
{
  "type": "status",
  "server_id": "pod-name-12345",
  "timestamp": "2025-08-07T10:30:00Z",
  "clients": 3
}
```

## Running Locally

```bash
go run main.go
```

Server starts on port 8080 (configurable via `PORT` environment variable).

## Docker

Build and run:
```bash
docker build -t ws-test-service .
docker run -p 8080:8080 ws-test-service
```

## Kubernetes Deployment

Deploy to Kubernetes:
```bash
kubectl apply -f k8s/
```

This creates:
- 3 replica deployment with pod identification
- LoadBalancer service exposing port 80
- Health checks and resource limits

## Testing

Test the WebSocket connection:
```bash
# Check status
curl http://localhost:8080/

# WebSocket test (requires wscat or similar)
wscat -c ws://localhost:8080/ws
```
