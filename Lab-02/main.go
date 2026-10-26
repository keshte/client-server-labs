package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// GatewayServer реалізує сервіс диспетчеризації та телеметрії
type GatewayServer struct {
	tcpPort     string
	udpPort     string
	tcpListener net.Listener
	udpConn     net.PacketConn

	clientsMu sync.RWMutex
	clients   map[string]net.Conn

	wg        sync.WaitGroup
	shutdown  chan struct{}
	isClosing atomic.Bool
}

// NewGatewayServer створює новий екземпляр шлюзу
func NewGatewayServer(tcpPort, udpPort string) *GatewayServer {
	return &GatewayServer{
		tcpPort:  tcpPort,
		udpPort:  udpPort,
		clients:  make(map[string]net.Conn),
		shutdown: make(chan struct{}),
	}
}

// Start запускає TCP та UDP слухачі
func (s *GatewayServer) Start() error {
	var err error

	// Відкриття TCP-слухача (Рівень 1)
	s.tcpListener, err = net.Listen("tcp", ":"+s.tcpPort)
	if err != nil {
		return fmt.Errorf("помилка запуску TCP-слухача: %w", err)
	}

	// Відкриття UDP-слухача (Рівень 3)
	s.udpConn, err = net.ListenPacket("udp", ":"+s.udpPort)
	if err != nil {
		s.tcpListener.Close()
		return fmt.Errorf("помилка запуску UDP-слухача: %w", err)
	}

	fmt.Printf("=== Мережевий сервіс диспетчеризації (TCP: %s, UDP: %s) ===\n", s.tcpPort, s.udpPort)
	fmt.Printf("[СЕРВЕР] TCP-слухач успішно відкрито на порту %s\n", s.tcpPort)
	fmt.Printf("[СЕРВЕР] UDP-слухач телеметрії відкрито на порту %s\n", s.udpPort)

	// Запуск прийому UDP-датаграм у фоновій горутині
	s.wg.Add(1)
	go s.listenUDP()

	// Запуск головного циклу прийому TCP-з'єднань
	s.wg.Add(1)
	go s.acceptTCP()

	return nil
}

// acceptTCP очікує нові підключення клієнтів (модель «горутина на кожне з'єднання»)
func (s *GatewayServer) acceptTCP() {
	defer s.wg.Done()

	for {
		conn, err := s.tcpListener.Accept()
		if err != nil {
			if s.isClosing.Load() {
				return
			}
			continue
		}

		s.wg.Add(1)
		go s.handleClient(conn)
	}
}

// handleClient обслуговує окреме TCP-з'єднання клієнта
func (s *GatewayServer) handleClient(conn net.Conn) {
	defer s.wg.Done()
	remoteAddr := conn.RemoteAddr().String()

	// Потокобезпечна реєстрація клієнта (Рівень 2)
	s.registerClient(remoteAddr, conn)
	fmt.Printf("[ПІДКЛЮЧЕННЯ] Новий клієнт підключився: %s\n", remoteAddr)

	// Системне вітальне повідомлення клієнту
	fmt.Fprintln(conn, "[СЕРВЕР] Ласкаво просимо до мережевого сервісу диспетчеризації!")

	// Сповіщення інших учасників про нове підключення
	s.Broadcast(fmt.Sprintf("[СИСТЕМА] Підключився новий клієнт: %s", remoteAddr), remoteAddr)

	// Гарантоване видалення клієнта при завершенні з'єднання
	defer func() {
		s.unregisterClient(remoteAddr)
		conn.Close()
		if !s.isClosing.Load() {
			fmt.Printf("[ВІДКЛЮЧЕННЯ] Клієнт відключився: %s\n", remoteAddr)
			s.Broadcast(fmt.Sprintf("[СИСТЕМА] Клієнт відключився: %s", remoteAddr), remoteAddr)
		}
	}()

	reader := bufio.NewReader(conn)

	// Построкове читання команд
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF && !s.isClosing.Load() {
				// Мережевий розрив або помилка читання
			}
			return
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		parts := strings.SplitN(trimmed, " ", 2)
		cmd := strings.ToUpper(parts[0])
		arg := ""
		if len(parts) > 1 {
			arg = strings.TrimSpace(parts[1])
		}

		switch cmd {
		case "ECHO":
			fmt.Fprintf(conn, "[ECHO]: %s\n", arg)

		case "TIME":
			fmt.Fprintf(conn, "[TIME]: %s\n", time.Now().Format(time.RFC3339))

		case "CLOSE", "QUIT":
			fmt.Fprintln(conn, "[СЕРВЕР] До побачення!")
			return

		case "BROADCAST", "MSG":
			// Широкомовна розсилка повідомлення всім іншим клієнтам (Рівень 2)
			msg := fmt.Sprintf("[СПОВІЩЕННЯ від %s]: %s", remoteAddr, arg)
			s.Broadcast(msg, remoteAddr)
			fmt.Printf("[РОЗСИЛКА] Від %s -> \"%s\"\n", remoteAddr, arg)

		default:
			fmt.Fprintln(conn, "[ПОМИЛКА]: Невідома команда. Доступні команди: ECHO <повідомлення>, TIME, BROADCAST <повідомлення>, MSG <повідомлення>, CLOSE, QUIT")
		}
	}
}

// listenUDP вичитує UDP датаграми телеметрії та транслює їх TCP-клієнтам (Рівень 3)
func (s *GatewayServer) listenUDP() {
	defer s.wg.Done()
	buffer := make([]byte, 2048)

	for {
		n, clientAddr, err := s.udpConn.ReadFrom(buffer)
		if err != nil {
			if s.isClosing.Load() {
				return
			}
			continue
		}

		data := strings.TrimSpace(string(buffer[:n]))
		fmt.Printf("[UDP ТЕЛЕМЕТРІЯ] Отримано %d байт від %s -> \"%s\"\n", n, clientAddr.String(), data)

		// Трансляція телеметрії всім підключеним TCP-клієнтам
		telemetryMsg := fmt.Sprintf("[TELEMETRY from %s]: %s", clientAddr.String(), data)
		s.BroadcastAll(telemetryMsg)
	}
}

// registerClient додає клієнта до списку активних підключень
func (s *GatewayServer) registerClient(addr string, conn net.Conn) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	s.clients[addr] = conn
}

// unregisterClient видаляє клієнта зі списку активних підключень
func (s *GatewayServer) unregisterClient(addr string) {
	s.clientsMu.Lock()
	defer s.clientsMu.Unlock()
	delete(s.clients, addr)
}

// Broadcast надсилає повідомлення усім клієнтам, крім відправника (senderAddr)
func (s *GatewayServer) Broadcast(message string, senderAddr string) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	data := []byte(message + "\n")
	for addr, conn := range s.clients {
		if addr == senderAddr {
			continue
		}
		_, _ = conn.Write(data)
	}
}

// BroadcastAll надсилає повідомлення усім активним TCP-клієнтам
func (s *GatewayServer) BroadcastAll(message string) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	data := []byte(message + "\n")
	for _, conn := range s.clients {
		_, _ = conn.Write(data)
	}
}

// Shutdown виконує плавне завершення роботи сервера (Graceful Shutdown, Рівень 3)
func (s *GatewayServer) Shutdown() {
	if !s.isClosing.CompareAndSwap(false, true) {
		return
	}

	fmt.Println("\n[СЕРВЕР] Отримано сигнал завершення роботи. Запуск Graceful Shutdown...")

	// Сповіщення всіх активних TCP-клієнтів про зупинку сервера
	s.clientsMu.Lock()
	clientCount := len(s.clients)
	shutdownMsg := []byte("[СИСТЕМА]: SERVER_SHUTTING_DOWN\n")

	for _, conn := range s.clients {
		_, _ = conn.Write(shutdownMsg)
		conn.Close()
	}
	s.clients = make(map[string]net.Conn)
	s.clientsMu.Unlock()

	// Закриття мережевих слухачів
	if s.tcpListener != nil {
		s.tcpListener.Close()
	}
	if s.udpConn != nil {
		s.udpConn.Close()
	}

	// Очікування завершення горутин
	s.wg.Wait()

	fmt.Printf("[СЕРВЕР] Сповіщено активних клієнтів: %d. З'єднання безпечно закриті.\n", clientCount)
	fmt.Println("[СЕРВЕР] Роботу сервера успішно завершено.")
}

func main() {
	tcpPort := flag.String("tcp", "9000", "Порт для TCP-з'єднань")
	udpPort := flag.String("udp", "9001", "Порт для UDP-телеметрії")
	flag.Parse()

	server := NewGatewayServer(*tcpPort, *udpPort)
	if err := server.Start(); err != nil {
		fmt.Printf("Помилка старту сервера: %v\n", err)
		os.Exit(1)
	}

	// Налаштування перехоплення системних сигналів (Ctrl+C, SIGTERM)
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)

	// Блокування головного потоку до надходження сигналу
	<-stopChan

	// Плавна зупинка
	server.Shutdown()
}
