package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type Config struct {
	Endpoint    string        `mapstructure:"endpoint"`
	Connections int           `mapstructure:"connections"`
	Timeout     time.Duration `mapstructure:"timeout"`
	ReadTimeout time.Duration `mapstructure:"read-timeout"`
	Headers     []string      `mapstructure:"headers"`
}

type Message struct {
	Type      string    `json:"type"`
	ServerID  string    `json:"server_id"`
	Timestamp time.Time `json:"timestamp"`
	Clients   int       `json:"clients,omitempty"`
}

type ConnectionStatus string

const (
	StatusConnecting   ConnectionStatus = "Connecting"
	StatusConnected    ConnectionStatus = "Connected"
	StatusDisconnected ConnectionStatus = "Disconnected"
	StatusError        ConnectionStatus = "Error"
)

type ConnectionInfo struct {
	ConnID   int
	Status   ConnectionStatus
	ServerID string
	LastSeen time.Time
}

type ServerInfo struct {
	ServerID      string
	Status        ConnectionStatus
	Connections   map[int]*ConnectionInfo
	ServerClients int
	LastUpdate    time.Time
	LastMessage   *Message
}

type LogMessage struct {
	Timestamp time.Time
	ConnID    int
	Type      string
	Message   string
}

type Client struct {
	config        Config
	dialer        *websocket.Dialer
	servers       map[string]*ServerInfo
	connections   map[int]*ConnectionInfo
	serverMux     sync.RWMutex
	connectionMux sync.RWMutex
	ui            *tea.Program
	logMessages   []LogMessage
	logMutex      sync.RWMutex
}

type UpdateMsg struct {
	ConnID   int
	ServerID string
	Status   ConnectionStatus
	Message  *Message
	LogMsg   *LogMessage
}

type ShutdownMsg struct{}

type WindowSizeMsg struct {
	Width  int
	Height int
}

type model struct {
	client      *Client
	table       table.Model
	ctx         context.Context
	cancel      context.CancelFunc
	quitting    bool
	width       int
	height      int
	logMessages []string
}

var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

func waitForSignal() tea.Cmd {
	return func() tea.Msg {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		<-c
		return ShutdownMsg{}
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(waitForSignal(), tea.WindowSize())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table = m.buildTable()
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			m.cancel()
			return m, tea.Quit
		case "r":
			return m, nil
		case "c":
			(&m).clearLogs()
			return m, nil
		}
	case ShutdownMsg:
		m.quitting = true
		m.cancel()
		return m, tea.Quit
	case UpdateMsg:
		if !m.quitting {
			m.client.updateServer(msg)
			if msg.LogMsg != nil {
				(&m).addLogMessage(msg.LogMsg)
			}
			m.table = m.buildTable()
		}
	}
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m *model) addLogMessage(logMsg *LogMessage) {
	timestamp := logMsg.Timestamp.Format("15:04:05")
	formatted := fmt.Sprintf("[%s] Conn%d %s: %s", timestamp, logMsg.ConnID, logMsg.Type, logMsg.Message)
	m.logMessages = append(m.logMessages, formatted)

	if len(m.logMessages) > 100 {
		m.logMessages = m.logMessages[1:]
	}
}

func (m *model) clearLogs() {
	m.logMessages = make([]string, 0)
}

func (m model) View() string {
	width := m.width
	height := m.height
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}

	header := m.buildHeader(width)

	headerContentLines := 3
	headerBorderLines := 2
	actualHeaderHeight := headerContentLines + headerBorderLines
	tableHeaderHeight := 3
	messagesPrefixHeight := 1
	messagePaneBorderLines := 2
	tmuxStatusBarBuffer := 2

	totalReservedHeight := actualHeaderHeight + tableHeaderHeight + messagesPrefixHeight + messagePaneBorderLines + tmuxStatusBarBuffer + 1
	availableHeight := height - totalReservedHeight

	if availableHeight < 4 {
		availableHeight = 4
	}

	tableHeight := availableHeight / 3
	if tableHeight < 3 {
		tableHeight = 3
	}

	maxSafeMessageHeight := height - actualHeaderHeight - tableHeaderHeight - messagesPrefixHeight - messagePaneBorderLines - 3
	messageHeight := availableHeight - tableHeight
	if messageHeight > maxSafeMessageHeight {
		messageHeight = maxSafeMessageHeight
	}
	if messageHeight < 1 {
		messageHeight = 1
	}
	var visibleMessages []string
	totalMessages := len(m.logMessages)
	startIdx := 0
	if totalMessages > messageHeight {
		startIdx = totalMessages - messageHeight
	}
	if totalMessages > 0 {
		visibleMessages = m.logMessages[startIdx:]
	}

	messagePaneContent := ""
	for _, msg := range visibleMessages {
		if len(msg) > width-2 {
			msg = msg[:width-5] + "..."
		}
		messagePaneContent += msg + "\n"
	}

	for len(visibleMessages) < messageHeight {
		messagePaneContent += "\n"
		visibleMessages = append(visibleMessages, "")
	}
	messagePane := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Width(width - 2).
		Height(messageHeight).
		Render(messagePaneContent)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		baseStyle.Width(width-2).Render(m.table.View()),
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("Messages:"),
		messagePane,
	)
}

func (m model) buildHeader(width int) string {
	connected, total := m.getConnectionSummary()

	statusColor := "1"
	statusText := "DISCONNECTED"
	if connected > 0 {
		if connected == total {
			statusColor = "2"
			statusText = "CONNECTED"
		} else {
			statusColor = "3"
			statusText = "PARTIAL"
		}
	}

	line1 := fmt.Sprintf("WebSocket Client | Endpoint: %s", m.client.config.Endpoint)
	line2 := fmt.Sprintf("Status: %s | Connections: %d/%d | Terminal: %dx%d",
		lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Bold(true).Render(statusText),
		connected, total, width, m.height)
	line3 := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("Controls: [q]uit • [r]econnect • [c]lear logs")

	headerContent := lipgloss.JoinVertical(
		lipgloss.Left,
		line1,
		line2,
		line3,
	)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")).
		Padding(0, 1).
		Width(width - 2).
		Render(headerContent)
}

func (m model) getConnectionSummary() (connected, total int) {
	m.client.serverMux.RLock()
	defer m.client.serverMux.RUnlock()

	total = m.client.config.Connections
	connected = 0

	for _, server := range m.client.servers {
		for _, conn := range server.Connections {
			if conn.Status == StatusConnected {
				connected++
			}
		}
	}

	return connected, total
}
func (m model) buildTable() table.Model {
	columns := []table.Column{
		{Title: "Server ID", Width: 40},
		{Title: "Status", Width: 20},
		{Title: "Connections", Width: 12},
		{Title: "Clients", Width: 8},
		{Title: "Last Update", Width: 12},
	}

	m.client.serverMux.RLock()
	defer m.client.serverMux.RUnlock()

	var rows []table.Row
	for serverID, server := range m.client.servers {
		if serverID == "Unknown" && m.client.getActiveConnections(server) == 0 {
			continue
		}

		statusColor := "9"
		switch server.Status {
		case StatusConnected:
			statusColor = "2"
		case StatusConnecting:
			statusColor = "3"
		case StatusError:
			statusColor = "1"
		case StatusDisconnected:
			statusColor = "8"
		}

		lastUpdate := ""
		if !server.LastUpdate.IsZero() {
			lastUpdate = server.LastUpdate.Format("15:04:05")
		}

		activeConnections := m.client.getActiveConnections(server)
		clientsStr := "-"
		if server.ServerClients > 0 {
			clientsStr = fmt.Sprintf("%d", server.ServerClients)
		}

		rows = append(rows, table.Row{
			server.ServerID,
			lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor)).Render(string(server.Status)),
			fmt.Sprintf("%d", activeConnections),
			clientsStr,
			lastUpdate,
		})
	}

	tableHeight := 10
	modelHeight := m.height
	if modelHeight == 0 {
		modelHeight = 24
	}
	if modelHeight > 0 {
		availableHeight := modelHeight - 6
		tableHeight = availableHeight / 3
		if tableHeight < 3 {
			tableHeight = 3
		}
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(tableHeight),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return t
}

func NewClient(config Config) *Client {
	return &Client{
		config: config,
		dialer: &websocket.Dialer{
			HandshakeTimeout: config.Timeout,
		},
		servers:     make(map[string]*ServerInfo),
		connections: make(map[int]*ConnectionInfo),
	}
}

func (c *Client) updateServer(msg UpdateMsg) {
	c.connectionMux.Lock()
	c.serverMux.Lock()
	defer c.connectionMux.Unlock()
	defer c.serverMux.Unlock()

	conn, exists := c.connections[msg.ConnID]
	if !exists {
		conn = &ConnectionInfo{
			ConnID:   msg.ConnID,
			ServerID: "Unknown",
		}
		c.connections[msg.ConnID] = conn
	}

	oldServerID := conn.ServerID
	conn.Status = msg.Status
	conn.LastSeen = time.Now()

	if msg.Message != nil && msg.Message.ServerID != "" {
		conn.ServerID = msg.Message.ServerID
	}

	if oldServerID != conn.ServerID {

		if oldServer, exists := c.servers[oldServerID]; exists {
			delete(oldServer.Connections, msg.ConnID)

			if len(oldServer.Connections) == 0 && oldServerID != "Unknown" {
				delete(c.servers, oldServerID)
			}
		}
	}

	server, exists := c.servers[conn.ServerID]
	if !exists {
		server = &ServerInfo{
			ServerID:    conn.ServerID,
			Status:      StatusConnecting,
			Connections: make(map[int]*ConnectionInfo),
		}
		c.servers[conn.ServerID] = server
	}

	server.Connections[msg.ConnID] = conn

	if msg.Message != nil {
		server.ServerClients = msg.Message.Clients
		server.LastMessage = msg.Message
		server.LastUpdate = time.Now()
	}

	server.Status = c.computeServerStatus(server)

	if unknownServer, exists := c.servers["Unknown"]; exists && len(unknownServer.Connections) == 0 {
		delete(c.servers, "Unknown")
	}
}
func (c *Client) computeServerStatus(server *ServerInfo) ConnectionStatus {
	if len(server.Connections) == 0 {
		return StatusDisconnected
	}

	connected := 0
	connecting := 0

	for _, conn := range server.Connections {
		switch conn.Status {
		case StatusConnected:
			connected++
		case StatusConnecting:
			connecting++
		}
	}

	if connected > 0 {
		return StatusConnected
	} else if connecting > 0 {
		return StatusConnecting
	} else {
		return StatusDisconnected
	}
}

func (c *Client) getActiveConnections(server *ServerInfo) int {
	count := 0
	for _, conn := range server.Connections {
		if conn.Status == StatusConnected {
			count++
		}
	}
	return count
}

func (c *Client) sendUpdate(connID int, status ConnectionStatus, msg *Message) {
	logMsg := &LogMessage{
		Timestamp: time.Now(),
		ConnID:    connID,
		Type:      "CONNECTION",
		Message:   fmt.Sprintf("Status: %s", status),
	}

	if msg != nil {
		logMsg.Type = strings.ToUpper(msg.Type)
		logMsg.Message = fmt.Sprintf("Server: %s, Clients: %d", msg.ServerID, msg.Clients)
	}

	if c.ui != nil {
		c.ui.Send(UpdateMsg{
			ConnID:  connID,
			Status:  status,
			Message: msg,
			LogMsg:  logMsg,
		})
	}
}

func (c *Client) connect(ctx context.Context, connID int, wg *sync.WaitGroup) {
	defer wg.Done()

	c.sendUpdate(connID, StatusConnecting, nil)

	u, err := url.Parse(c.config.Endpoint)
	if err != nil {

		if c.ui != nil {
			c.ui.Send(UpdateMsg{
				ConnID: connID,
				Status: StatusError,
				LogMsg: &LogMessage{
					Timestamp: time.Now(),
					ConnID:    connID,
					Type:      "ERROR",
					Message:   fmt.Sprintf("Failed to parse URL: %v", err),
				},
			})
		}
		c.sendUpdate(connID, StatusError, nil)
		return
	}

	headers := http.Header{
		"User-Agent": []string{"ws-test-client/1.0 (Go WebSocket Test Tool)"},
	}

	for _, header := range c.config.Headers {
		parts := strings.SplitN(header, ":", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			if key != "" && value != "" {
				headers.Add(key, value)
			}
		}
	}

	conn, _, err := c.dialer.DialContext(ctx, u.String(), headers)
	if err != nil {

		if c.ui != nil {
			c.ui.Send(UpdateMsg{
				ConnID: connID,
				Status: StatusError,
				LogMsg: &LogMessage{
					Timestamp: time.Now(),
					ConnID:    connID,
					Type:      "ERROR",
					Message:   fmt.Sprintf("Connection failed: %v", err),
				},
			})
		}
		c.sendUpdate(connID, StatusError, nil)
		return
	}
	defer conn.Close()

	if c.ui != nil {
		c.ui.Send(UpdateMsg{
			ConnID: connID,
			Status: StatusConnected,
			LogMsg: &LogMessage{
				Timestamp: time.Now(),
				ConnID:    connID,
				Type:      "CONNECTED",
				Message:   fmt.Sprintf("Connected to %s", c.config.Endpoint),
			},
		})
	}
	c.sendUpdate(connID, StatusConnected, nil)

	conn.SetPingHandler(func(appData string) error {

		conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))

		if c.ui != nil {
			c.ui.Send(UpdateMsg{
				ConnID: connID,
				Status: StatusConnected,
				LogMsg: &LogMessage{
					Timestamp: time.Now(),
					ConnID:    connID,
					Type:      "PING",
					Message:   "Received ping from server",
				},
			})
		}

		err := conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(10*time.Second))

		if c.ui != nil {
			c.ui.Send(UpdateMsg{
				ConnID: connID,
				Status: StatusConnected,
				LogMsg: &LogMessage{
					Timestamp: time.Now(),
					ConnID:    connID,
					Type:      "PONG",
					Message:   "Sent pong response",
				},
			})
		}

		return err
	})
	conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))

		if c.ui != nil {
			c.ui.Send(UpdateMsg{
				ConnID: connID,
				Status: StatusConnected,
				LogMsg: &LogMessage{
					Timestamp: time.Now(),
					ConnID:    connID,
					Type:      "PONG",
					Message:   "Received pong from server",
				},
			})
		}

		return nil
	})

	go func() {
		<-ctx.Done()
		conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		conn.Close()
	}()

	welcomeReceived := false

	conn.SetReadDeadline(time.Now().Add(30 * time.Second))

	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			select {
			case <-ctx.Done():

				if c.ui != nil {
					c.ui.Send(UpdateMsg{
						ConnID: connID,
						Status: StatusDisconnected,
						LogMsg: &LogMessage{
							Timestamp: time.Now(),
							ConnID:    connID,
							Type:      "DISCONNECTED",
							Message:   "Connection closed gracefully",
						},
					})
				}
				c.sendUpdate(connID, StatusDisconnected, nil)
				return
			default:
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {

					if c.ui != nil {
						c.ui.Send(UpdateMsg{
							ConnID: connID,
							Status: StatusError,
							LogMsg: &LogMessage{
								Timestamp: time.Now(),
								ConnID:    connID,
								Type:      "ERROR",
								Message:   fmt.Sprintf("Unexpected disconnection: %v", err),
							},
						})
					}
					c.sendUpdate(connID, StatusError, nil)
				} else {

					if c.ui != nil {
						c.ui.Send(UpdateMsg{
							ConnID: connID,
							Status: StatusDisconnected,
							LogMsg: &LogMessage{
								Timestamp: time.Now(),
								ConnID:    connID,
								Type:      "DISCONNECTED",
								Message:   "Connection closed",
							},
						})
					}
					c.sendUpdate(connID, StatusDisconnected, nil)
				}
				return
			}
		}

		if !welcomeReceived {
			welcomeReceived = true
			conn.SetReadDeadline(time.Now().Add(c.config.ReadTimeout))
		}

		c.sendUpdate(connID, StatusConnected, &msg)
	}
}

func initConfig() Config {
	pflag.String("endpoint", "ws://localhost:8080/ws", "WebSocket server endpoint")
	pflag.Int("connections", 1, "Number of concurrent connections")
	pflag.Duration("timeout", 10*time.Second, "Connection timeout")
	pflag.Duration("read-timeout", 60*time.Second, "Read timeout for messages")
	pflag.StringArrayP("header", "H", []string{}, "Custom header to send (can be used multiple times, format: 'Key: Value')")
	pflag.Parse()

	viper.BindPFlags(pflag.CommandLine)
	viper.SetEnvPrefix("WS")
	viper.AutomaticEnv()

	viper.SetDefault("endpoint", "ws://localhost:8080/ws")
	viper.SetDefault("connections", 1)
	viper.SetDefault("timeout", 10*time.Second)
	viper.SetDefault("read-timeout", 60*time.Second)
	viper.SetDefault("headers", []string{})

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		fmt.Printf("Unable to decode config: %v\n", err)
		os.Exit(1)
	}

	return config
}

func main() {
	config := initConfig()
	client := NewClient(config)

	ctx, cancel := context.WithCancel(context.Background())

	m := model{
		client:      client,
		table:       client.buildInitialTable(),
		ctx:         ctx,
		cancel:      cancel,
		logMessages: make([]string, 0),
		width:       80,
		height:      24,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	client.ui = p

	var wg sync.WaitGroup
	for i := 1; i <= config.Connections; i++ {
		wg.Add(1)
		go client.connect(ctx, i, &wg)
	}

	if _, err := p.Run(); err != nil {
		fmt.Printf("Error running program: %v\n", err)
		os.Exit(1)
	}

	cancel()

	wg.Wait()
}

func (c *Client) buildInitialTable() table.Model {
	columns := []table.Column{
		{Title: "Server ID", Width: 40},
		{Title: "Status", Width: 12},
		{Title: "Connections", Width: 12},
		{Title: "Clients", Width: 8},
		{Title: "Last Update", Width: 12},
	}

	rows := []table.Row{
		{
			"Connecting...",
			lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("Connecting"),
			"0",
			"-",
			"-",
		},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(false),
		table.WithHeight(3),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	t.SetStyles(s)

	return t
}
