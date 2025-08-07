# WebSocket Test Suite

A complete WebSocket testing suite designed for testing load balancing strategies with WebSocket connections. This tool helps validate that load balancers properly distribute WebSocket connections across multiple server instances and maintain session affinity when required.

The suite includes both server and client components written in Go. The server provides WebSocket connections with identification and statistics, while the client offers an interactive terminal UI for testing multiple concurrent connections and observing load distribution.

## Components

### Server
- **WebSocket Endpoint** (`/ws`): Accepts WebSocket connections and sends server identification
- **HTTP Status Endpoint** (`/`): Returns JSON with server ID and connected client count
- **Kubernetes Support**: Automatically detects pod name when running in Kubernetes
- **Client Tracking**: Thread-safe tracking of concurrent WebSocket connections
- **Ping/Pong Support**: Configurable heartbeat mechanism with timeout handling

### Client
- **Interactive Terminal UI**: Real-time dashboard showing connection status and server information
- **Multi-Connection Support**: Test with multiple concurrent connections
- **Server Aggregation**: Groups connections by server ID for easy monitoring
- **Connection Logging**: Real-time message and event logging
- **Configurable Options**: Customizable endpoints, timeouts, and headers

## Server Identification

The service identifies itself using the first available option:
1. `POD_NAME` environment variable (set by Kubernetes)
2. `HOSTNAME` environment variable
3. System hostname
4. Fallback to generated UUID (e.g., "a1b2c3d4-e5f6-4789-8abc-def012345678")

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

## Running the Server

### Locally
```bash
cd server
go run main.go
```

Server starts on port 8080 (configurable via `PORT` environment variable).

### Environment Variables
- `PORT`: Server port (default: 8080)
- `POD_NAME`: Pod name in Kubernetes (auto-detected)
- `PING_INTERVAL`: WebSocket ping interval (default: 30s)
- `PONG_TIMEOUT`: Pong response timeout (default: 10s)

## Running the Client

### Locally
```bash
cd client
go run main.go
```

### Command Line Options
```bash
# Basic usage
go run main.go --endpoint ws://localhost:8080/ws

# Multiple connections
go run main.go --connections 5 --endpoint ws://your-server:8080/ws

# Custom headers and timeouts
go run main.go --header "Authorization: Bearer token" --timeout 30s --read-timeout 120s
```

### Available Options
- `--endpoint`: WebSocket server endpoint (default: ws://localhost:8080/ws)
- `--connections`: Number of concurrent connections (default: 1)
- `--timeout`: Connection timeout (default: 10s)
- `--read-timeout`: Read timeout for messages (default: 60s)
- `--header, -H`: Custom headers (format: 'Key: Value', can be used multiple times)

### Environment Variables
All CLI options can be set via environment variables with `WS_` prefix:
- `WS_ENDPOINT`
- `WS_CONNECTIONS`
- `WS_TIMEOUT`
- `WS_READ_TIMEOUT`
- `WS_HEADERS`

### Interactive Controls
- `q` or `Ctrl+C`: Quit the application
- `r`: Reconnect all connections
- `c`: Clear message logs

## Docker

Build and run:
```bash
docker build -t ws-server .
docker run -p 8080:8080 ws-server
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

### Quick Server Test
```bash
# Check status endpoint
curl http://localhost:8080/

# WebSocket test with wscat
wscat -c ws://localhost:8080/ws
```

### Using the Interactive Client
The included client provides the best testing experience:
```bash
cd client
go run main.go --connections 3 --endpoint ws://localhost:8080/ws
```

This will show a real-time dashboard with:
- Connection status for each server
- Message logs and ping/pong activity
- Server-reported client counts
- Terminal-based interface with live updates
