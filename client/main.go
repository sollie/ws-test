package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Endpoint    string        `mapstructure:"endpoint"`
	Connections int           `mapstructure:"connections"`
	Timeout     time.Duration `mapstructure:"timeout"`
	ReadTimeout time.Duration `mapstructure:"read-timeout"`
}

type Message struct {
	Type      string    `json:"type"`
	ServerID  string    `json:"server_id"`
	Timestamp time.Time `json:"timestamp"`
	Clients   int       `json:"clients,omitempty"`
}

type Client struct {
	config Config
	dialer *websocket.Dialer
}

func NewClient(config Config) *Client {
	return &Client{
		config: config,
		dialer: &websocket.Dialer{
			HandshakeTimeout: config.Timeout,
		},
	}
}

func (c *Client) connect(ctx context.Context, connID int, wg *sync.WaitGroup) {
	defer wg.Done()

	u, err := url.Parse(c.config.Endpoint)
	if err != nil {
		log.Printf("Connection %d: Invalid endpoint URL: %v", connID, err)
		return
	}

	log.Printf("Connection %d: Connecting to %s", connID, u.String())

	conn, _, err := c.dialer.DialContext(ctx, u.String(), nil)
	if err != nil {
		log.Printf("Connection %d: Failed to connect: %v", connID, err)
		return
	}
	defer conn.Close()

	log.Printf("Connection %d: Connected successfully", connID)

	// Set up ping handler (respond to server pings with pong)
	conn.SetPingHandler(func(appData string) error {
		log.Printf("Connection %d: Received ping, sending pong", connID)
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))
	})

	// Set read deadline
	conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
		return nil
	})

	// Handle context cancellation in a separate goroutine
	go func() {
		<-ctx.Done()
		log.Printf("Connection %d: Context cancelled, closing connection", connID)
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
	}()

	// Message reading loop
	for {
		// Set a shorter read deadline to make reads interruptible
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			select {
			case <-ctx.Done():
				log.Printf("Connection %d: Shutdown requested", connID)
				return
			default:
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
					log.Printf("Connection %d: WebSocket error: %v", connID, err)
				} else {
					log.Printf("Connection %d: Connection closed: %v", connID, err)
				}
				return
			}
		}

		// Output JSON message
		jsonBytes, _ := json.MarshalIndent(msg, "", "  ")
		log.Printf("Connection %d: Received message:\n%s", connID, string(jsonBytes))

		// Reset read deadline on successful message
		conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
	}
}

func initConfig() Config {
	// Define command line flags
	pflag.String("endpoint", "ws://localhost:8080/ws", "WebSocket server endpoint")
	pflag.Int("connections", 1, "Number of concurrent connections")
	pflag.Duration("timeout", 10*time.Second, "Connection timeout")
	pflag.Duration("read-timeout", 60*time.Second, "Read timeout for messages")
	pflag.Parse()

	// Bind flags to viper
	viper.BindPFlags(pflag.CommandLine)

	// Set environment variable prefix
	viper.SetEnvPrefix("WS")
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("endpoint", "ws://localhost:8080/ws")
	viper.SetDefault("connections", 1)
	viper.SetDefault("timeout", 10*time.Second)
	viper.SetDefault("read-timeout", 60*time.Second)

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		log.Fatalf("Unable to decode config: %v", err)
	}

	return config
}

func main() {
	config := initConfig()

	log.Printf("Starting WebSocket client with config:")
	log.Printf("  Endpoint: %s", config.Endpoint)
	log.Printf("  Connections: %d", config.Connections)
	log.Printf("  Timeout: %v", config.Timeout)
	log.Printf("  Read Timeout: %v", config.ReadTimeout)

	client := NewClient(config)

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Printf("Shutdown signal received, closing connections...")
		cancel()
	}()

	// Start connections
	var wg sync.WaitGroup
	for i := 1; i <= config.Connections; i++ {
		wg.Add(1)
		go client.connect(ctx, i, &wg)
	}

	// Wait for all connections to finish
	wg.Wait()
	log.Printf("All connections closed")
}
