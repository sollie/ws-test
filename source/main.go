package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Server struct {
	upgrader     websocket.Upgrader
	clients      map[*websocket.Conn]bool
	clientsMutex sync.RWMutex
	serverID     string
}

type Message struct {
	Type      string    `json:"type"`
	ServerID  string    `json:"server_id"`
	Timestamp time.Time `json:"timestamp"`
	Clients   int       `json:"clients,omitempty"`
}

func NewServer() *Server {
	serverID := getServerID()

	return &Server{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
		clients:  make(map[*websocket.Conn]bool),
		serverID: serverID,
	}
}

func getServerID() string {
	if podName := os.Getenv("POD_NAME"); podName != "" {
		return podName
	}

	if hostname := os.Getenv("HOSTNAME"); hostname != "" {
		return hostname
	}

	if hostname, err := os.Hostname(); err == nil {
		return hostname
	}

	return "unknown-server"
}

func (s *Server) addClient(conn *websocket.Conn) {
	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()
	s.clients[conn] = true
}

func (s *Server) removeClient(conn *websocket.Conn) {
	s.clientsMutex.Lock()
	defer s.clientsMutex.Unlock()
	delete(s.clients, conn)
	conn.Close()
}

func (s *Server) getClientCount() int {
	s.clientsMutex.RLock()
	defer s.clientsMutex.RUnlock()
	return len(s.clients)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	s.addClient(conn)
	log.Printf("Client connected. Total clients: %d", s.getClientCount())

	welcomeMsg := Message{
		Type:      "welcome",
		ServerID:  s.serverID,
		Timestamp: time.Now(),
		Clients:   s.getClientCount(),
	}

	if err := conn.WriteJSON(welcomeMsg); err != nil {
		log.Printf("Error sending welcome message: %v", err)
		s.removeClient(conn)
		return
	}

	defer func() {
		s.removeClient(conn)
		log.Printf("Client disconnected. Total clients: %d", s.getClientCount())
	}()

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			break
		}

		statusMsg := Message{
			Type:      "status",
			ServerID:  s.serverID,
			Timestamp: time.Now(),
			Clients:   s.getClientCount(),
		}

		if err := conn.WriteJSON(statusMsg); err != nil {
			log.Printf("Error sending status message: %v", err)
			break
		}
	}
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := Message{
		Type:      "status",
		ServerID:  s.serverID,
		Timestamp: time.Now(),
		Clients:   s.getClientCount(),
	}

	json.NewEncoder(w).Encode(status)
}

func main() {
	server := NewServer()

	http.HandleFunc("/ws", server.handleWebSocket)
	http.HandleFunc("/", server.handleStatus)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server %s starting on port %s", server.serverID, port)
	log.Printf("WebSocket endpoint: /ws")
	log.Printf("Status endpoint: /")

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
