package main

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

// getFreePorts повертає вільні порти для ізольованого тестування
func getFreePorts(t *testing.T) (string, string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free tcp port: %v", err)
	}
	tcpPort := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()

	u, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to get free udp port: %v", err)
	}
	udpPort := fmt.Sprintf("%d", u.LocalAddr().(*net.UDPAddr).Port)
	_ = u.Close()

	return tcpPort, udpPort
}

func TestTCPCommandsAndEcho(t *testing.T) {
	tcpPort, udpPort := getFreePorts(t)
	server := NewGatewayServer(tcpPort, udpPort)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Shutdown()

	conn, err := net.Dial("tcp", "127.0.0.1:"+tcpPort)
	if err != nil {
		t.Fatalf("failed to connect to tcp server: %v", err)
	}
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Перевірка вітального повідомлення
	welcome, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read welcome message: %v", err)
	}
	if !strings.Contains(welcome, "[СЕРВЕР] Ласкаво просимо") {
		t.Errorf("unexpected welcome message: %s", welcome)
	}

	// Тест команди ECHO
	fmt.Fprintf(conn, "ECHO Hello World\n")
	echoResp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read echo response: %v", err)
	}
	if strings.TrimSpace(echoResp) != "[ECHO]: Hello World" {
		t.Errorf("unexpected echo response: %s", echoResp)
	}

	// Тест команди TIME
	fmt.Fprintf(conn, "TIME\n")
	timeResp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read time response: %v", err)
	}
	if !strings.HasPrefix(strings.TrimSpace(timeResp), "[TIME]:") {
		t.Errorf("unexpected time response: %s", timeResp)
	}

	// Тест невідомої команди
	fmt.Fprintf(conn, "UNKNOWN_CMD\n")
	errResp, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read error response: %v", err)
	}
	if !strings.Contains(errResp, "[ПОМИЛКА]") {
		t.Errorf("expected error response, got: %s", errResp)
	}
}

func TestBroadcastBetweenClients(t *testing.T) {
	tcpPort, udpPort := getFreePorts(t)
	server := NewGatewayServer(tcpPort, udpPort)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Shutdown()

	// Клієнт 1
	c1, err := net.Dial("tcp", "127.0.0.1:"+tcpPort)
	if err != nil {
		t.Fatalf("c1 failed to connect: %v", err)
	}
	defer c1.Close()
	r1 := bufio.NewReader(c1)
	_, _ = r1.ReadString('\n') // Пропускаємо Welcome

	// Клієнт 2
	c2, err := net.Dial("tcp", "127.0.0.1:"+tcpPort)
	if err != nil {
		t.Fatalf("c2 failed to connect: %v", err)
	}
	defer c2.Close()
	r2 := bufio.NewReader(c2)
	_, _ = r2.ReadString('\n') // Пропускаємо Welcome

	// Клієнт 1 повинен отримати повідомлення про підключення клієнта 2
	systemJoin, err := r1.ReadString('\n')
	if err != nil || !strings.Contains(systemJoin, "[СИСТЕМА] Підключився новий клієнт") {
		t.Fatalf("c1 did not receive join event: %v (%s)", err, systemJoin)
	}

	// Клієнт 1 надсилає BROADCAST
	fmt.Fprintf(c1, "BROADCAST Test broadcast message\n")

	// Клієнт 2 повинен отримати це повідомлення
	broadcastMsg, err := r2.ReadString('\n')
	if err != nil {
		t.Fatalf("c2 failed to read broadcast: %v", err)
	}
	if !strings.Contains(broadcastMsg, "Test broadcast message") {
		t.Errorf("c2 received unexpected broadcast message: %s", broadcastMsg)
	}
}

func TestUDPTelemetryBroadcast(t *testing.T) {
	tcpPort, udpPort := getFreePorts(t)
	server := NewGatewayServer(tcpPort, udpPort)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Shutdown()

	// Підключаємо TCP-клієнта (диспетчера)
	tcpConn, err := net.Dial("tcp", "127.0.0.1:"+tcpPort)
	if err != nil {
		t.Fatalf("failed to connect tcp client: %v", err)
	}
	defer tcpConn.Close()
	r := bufio.NewReader(tcpConn)
	_, _ = r.ReadString('\n') // Welcome

	// Відправляємо UDP-пакет (імітація датчика)
	udpConn, err := net.Dial("udp", "127.0.0.1:"+udpPort)
	if err != nil {
		t.Fatalf("failed to dial udp: %v", err)
	}
	defer udpConn.Close()

	sensorPayload := "temp=24.5;press=750"
	_, err = udpConn.Write([]byte(sensorPayload))
	if err != nil {
		t.Fatalf("failed to write udp packet: %v", err)
	}

	// Очікуємо надходження сповіщення телеметрії на TCP-клієнті
	telemetryMsg, err := r.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read telemetry on tcp client: %v", err)
	}

	if !strings.Contains(telemetryMsg, "[TELEMETRY from") || !strings.Contains(telemetryMsg, sensorPayload) {
		t.Errorf("unexpected telemetry broadcast message: %s", telemetryMsg)
	}
}

func TestGracefulShutdown(t *testing.T) {
	tcpPort, udpPort := getFreePorts(t)
	server := NewGatewayServer(tcpPort, udpPort)
	if err := server.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	conn, err := net.Dial("tcp", "127.0.0.1:"+tcpPort)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	defer conn.Close()
	reader := bufio.NewReader(conn)
	_, _ = reader.ReadString('\n') // Welcome

	// Запускаємо Shutdown
	done := make(chan struct{})
	go func() {
		server.Shutdown()
		close(done)
	}()

	// Клієнт повинен отримати SERVER_SHUTTING_DOWN і з'єднання має закритись
	shutdownNotice, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read shutdown notice: %v", err)
	}
	if !strings.Contains(shutdownNotice, "SERVER_SHUTTING_DOWN") {
		t.Errorf("unexpected shutdown message: %s", shutdownNotice)
	}

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown timed out")
	}
}
