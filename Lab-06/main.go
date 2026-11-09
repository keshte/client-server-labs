package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all CORS
	},
}

// --- Real-time Data Structures ---
type TelemetryMetric struct {
	Time       string  `json:"time"`
	Goroutines int     `json:"goroutines"`
	AllocKB    uint64  `json:"alloc_kb"`
	AllocMB    float64 `json:"alloc_mb"`
}

type ChatMessage struct {
	Room    string `json:"room"`
	Sender  string `json:"sender"`
	Content string `json:"content"`
	Time    string `json:"time"`
}

// --- Client & Hub ---
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	room string
	ip   string
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan ChatMessage
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan ChatMessage),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) Run() {
	log.Println("[СЕРВЕР] Диспетчер WebSocket Hub успішно запущено")
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("[WS] Новий клієнт підключився до кімнати [%s]: %s\n", client.room, client.ip)

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
				log.Printf("[WS] Клієнт відключився від кімнати [%s]: %s\n", client.room, client.ip)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			count := 0
			rawBytes, _ := json.Marshal(message)
			for client := range h.clients {
				// Broadcast strictly to clients in the same room
				if client.room == message.Room {
					select {
					case client.send <- rawBytes:
						count++
					default:
						close(client.send)
						delete(h.clients, client)
					}
				}
			}
			h.mu.RUnlock()
			log.Printf("[HUB BROADCAST] [Кімната: %s] Отримано повідомлення: \"%s\"\n", message.Room, message.Content)
			log.Printf("[HUB BROADCAST] Сповіщено клієнтів у кімнаті [%s]: %d\n", message.Room, count)
		}
	}
}

// --- Client Pumps ---
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		log.Printf("[HEARTBEAT] Pong отримано успішно від клієнта %s\n", c.ip)
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("[WS ERROR] Read error: %v", err)
			}
			break
		}

		var chatMsg ChatMessage
		if err := json.Unmarshal(message, &chatMsg); err != nil {
			// Plain text fallback
			chatMsg = ChatMessage{
				Room:    c.room,
				Sender:  c.ip,
				Content: string(message),
				Time:    time.Now().Format("15:04:05"),
			}
		} else {
			if chatMsg.Room == "" {
				chatMsg.Room = c.room
			}
			if chatMsg.Sender == "" {
				chatMsg.Sender = c.ip
			}
			chatMsg.Time = time.Now().Format("15:04:05")
		}

		c.hub.broadcast <- chatMsg
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			// Add queued messages to the same frame
			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
			log.Printf("[HEARTBEAT] Ping надіслано клієнту %s\n", c.ip)
		}
	}
}

// --- SSE Handler ---
func metricsStreamHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientIP := r.RemoteAddr
	log.Printf("[SSE] Нове підключення клієнта телеметрії: %s\n", clientIP)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Initial ping
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			log.Printf("[SSE] Клієнт %s розірвав з'єднання. Очищення ресурсів виконано.\n", clientIP)
			return

		case t := <-ticker.C:
			var m runtime.MemStats
			runtime.ReadMemStats(&m)

			metric := TelemetryMetric{
				Time:       t.Format(time.RFC3339),
				Goroutines: runtime.NumGoroutine(),
				AllocKB:    m.Alloc / 1024,
				AllocMB:    float64(m.Alloc) / (1024 * 1024),
			}

			data, _ := json.Marshal(metric)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// --- WebSocket Connection Endpoint ---
func wsHandler(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[WS] Upgrade failed: %v", err)
		return
	}

	room := r.URL.Query().Get("room")
	if strings.TrimSpace(room) == "" {
		room = "general"
	}

	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan []byte, 256),
		room: room,
		ip:   r.RemoteAddr,
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()
}

func main() {
	fmt.Println("=== Сервіс реального часу (SSE & WebSockets Gateway) ===")
	hub := newHub()
	go hub.Run()

	mux := http.NewServeMux()

	// 1. SSE Stream
	mux.HandleFunc("/api/v1/metrics/stream", metricsStreamHandler)

	// 2. WebSocket
	mux.HandleFunc("/api/v1/ws", func(w http.ResponseWriter, r *http.Request) {
		wsHandler(hub, w, r)
	})

	// 3. Static Web Client
	fs := http.FileServer(http.Dir("./web"))
	mux.Handle("/", fs)

	port := ":8080"
	fmt.Printf("[СЕРВЕР] HTTP-слухач відкрито на порту %s\n", port)
	fmt.Printf("[СЕРВЕР] Веб-панель доступна на: http://localhost%s\n", port)

	server := &http.Server{
		Addr:    port,
		Handler: mux,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server Listen error: %v", err)
	}
}
