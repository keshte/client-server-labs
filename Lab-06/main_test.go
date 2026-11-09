package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRealTimeGateway(t *testing.T) {
	hub := newHub()
	go hub.Run()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/v1/metrics/stream") {
			metricsStreamHandler(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/api/v1/ws") {
			wsHandler(hub, w, r)
		}
	}))
	defer server.Close()

	// 1. Test SSE Stream Endpoint
	t.Run("SSE Telemetry Stream", func(t *testing.T) {
		resp, err := http.Get(server.URL + "/api/v1/metrics/stream")
		if err != nil {
			t.Fatalf("Failed to connect to SSE stream: %v", err)
		}
		defer resp.Body.Close()

		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
			t.Fatalf("Expected Content-Type text/event-stream, got %s", ct)
		}

		reader := bufio.NewReader(resp.Body)
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("Failed to read SSE initial chunk: %v", err)
		}
		t.Logf("SSE Handshake received: %s", strings.TrimSpace(line))
	})

	// 2. Test WebSocket Hub & Room Isolation
	t.Run("WebSocket Room Isolation and Broadcasting", func(t *testing.T) {
		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")

		// Connect Client 1 to room 'alerts'
		conn1, _, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/ws?room=alerts", nil)
		if err != nil {
			t.Fatalf("Client 1 failed to connect: %v", err)
		}
		defer conn1.Close()

		// Connect Client 2 to room 'alerts'
		conn2, _, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/ws?room=alerts", nil)
		if err != nil {
			t.Fatalf("Client 2 failed to connect: %v", err)
		}
		defer conn2.Close()

		// Connect Client 3 to room 'general'
		conn3, _, err := websocket.DefaultDialer.Dial(wsURL+"/api/v1/ws?room=general", nil)
		if err != nil {
			t.Fatalf("Client 3 failed to connect: %v", err)
		}
		defer conn3.Close()

		time.Sleep(100 * time.Millisecond)

		// Client 1 sends message to 'alerts'
		alertMsg := ChatMessage{
			Room:    "alerts",
			Sender:  "Operator-1",
			Content: "Увага! Перегрів вузла node-03",
		}
		raw, _ := json.Marshal(alertMsg)
		if err := conn1.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatalf("Client 1 failed to send: %v", err)
		}

		// Client 2 (in 'alerts') MUST receive the message
		_ = conn2.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msgBytes2, err := conn2.ReadMessage()
		if err != nil {
			t.Fatalf("Client 2 failed to receive broadcast: %v", err)
		}

		var receivedMsg ChatMessage
		_ = json.Unmarshal(msgBytes2, &receivedMsg)
		if receivedMsg.Content != "Увага! Перегрів вузла node-03" {
			t.Fatalf("Unexpected message content: %s", receivedMsg.Content)
		}
		t.Logf("Client 2 (alerts) successfully received: %s", receivedMsg.Content)

		// Client 3 (in 'general') MUST NOT receive alerts message (timeout expected)
		_ = conn3.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		_, _, err3 := conn3.ReadMessage()
		if err3 == nil {
			t.Fatalf("Client 3 in 'general' should NOT receive message for 'alerts'")
		}
		t.Logf("Client 3 (general) correctly did not receive alerts broadcast (Room isolation verified)")
	})
}
