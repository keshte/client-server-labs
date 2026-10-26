package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"securegateway/email"
)

func findCertsDir() string {
	candidates := []string{
		"certs",
		"../certs",
		"../../certs",
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "ca.crt")); err == nil {
			return c
		}
	}
	return "certs"
}

func main() {
	port := flag.String("port", "8443", "Порт для mTLS-сервера")
	smtpHost := flag.String("smtp-host", "127.0.0.1", "Хост SMTP-сервера")
	smtpPort := flag.String("smtp-port", "1025", "Порт SMTP-сервера")
	recipient := flag.String("recipient", "duty-engineer@internal.net", "Адреса чергового інженера")
	flag.Parse()

	certsDir := findCertsDir()
	caFile := filepath.Join(certsDir, "ca.crt")
	serverCertFile := filepath.Join(certsDir, "server.crt")
	serverKeyFile := filepath.Join(certsDir, "server.key")

	fmt.Printf("=== Захищений шлюз критичних сповіщень (mTLS: %s, SMTP Target: %s) ===\n", *port, *smtpPort)

	// 1. Завантаження сертифіката та закритого ключа сервера (Рівень 1)
	serverCert, err := tls.LoadX509KeyPair(serverCertFile, serverKeyFile)
	if err != nil {
		fmt.Printf("[ПОМИЛКА] Не вдалося завантажити сертифікати сервера: %v\n", err)
		os.Exit(1)
	}

	// 2. Завантаження довіреного Root CA для верифікації клієнтів (Рівень 1)
	caCertPEM, err := os.ReadFile(caFile)
	if err != nil {
		fmt.Printf("[ПОМИЛКА] Не вдалося прочитати Root CA: %v\n", err)
		os.Exit(1)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		fmt.Println("[ПОМИЛКА] Не вдалося додати ca.crt у пул довірених сертифікатів")
		os.Exit(1)
	}
	fmt.Printf("[СЕРВЕР] Пул CA завантажено: %s (1 сертифікат)\n", filepath.Base(caFile))

	// 3. Конфігурація mTLS із вимогою обов'язкового клієнтського сертифіката
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert, // Критична опція mTLS (Рівень 1)
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS13,
	}

	listener, err := tls.Listen("tcp", ":"+*port, tlsConfig)
	if err != nil {
		fmt.Printf("[ПОМИЛКА] Не вдалося запустити mTLS-слухач: %v\n", err)
		os.Exit(1)
	}
	defer listener.Close()

	fmt.Printf("[СЕРВЕР] Слухач mTLS відкрито на порту :%s\n", *port)

	emailSvc := email.NewEmailService(*smtpHost, *smtpPort, "alert-gateway@internal.net", nil)

	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("[ПОМИЛКА] Помилка з'єднання: %v\n", err)
			continue
		}

		go handleClient(conn, emailSvc, *recipient)
	}
}

func handleClient(conn net.Conn, emailSvc *email.EmailService, recipient string) {
	defer conn.Close()
	remoteAddr := conn.RemoteAddr().String()
	fmt.Printf("[ПІДКЛЮЧЕННЯ] Встановлено TLS-з'єднання від %s\n", remoteAddr)

	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return
	}

	// Виконуємо явне рукостискання для перевірки стану
	if err := tlsConn.Handshake(); err != nil {
		fmt.Printf("[ВІДХИЛЕНО] Помилка TLS-рукостискання від %s: %v\n", remoteAddr, err)
		return
	}

	// Рівень 3: Аудит суб'єкта сертифіката (Zero Trust Identity)
	state := tlsConn.ConnectionState()
	clientCN := "Unknown"
	serialNumber := "Unknown"

	if len(state.PeerCertificates) > 0 {
		cert := state.PeerCertificates[0]
		clientCN = cert.Subject.CommonName
		serialNumber = cert.SerialNumber.String()
	}

	fmt.Printf("[АУДИТ ZERO TRUST] Клієнт верифікований: CN=%s, Серійний номер=%s\n", clientCN, serialNumber)

	// Читання повідомлення від авторизованого клієнта
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}

	incident := strings.TrimSpace(line)
	fmt.Printf("[ПОВІДОМЛЕННЯ] Отримано інцидент: %s\n", incident)

	// Рівень 2 та 3: Формування та відправка поштового сповіщення через SMTP
	now := time.Now()
	fmt.Println("[SMTP] Формування MIME-повідомлення (HTML + вкладення incident_report.log)...")

	htmlBody := email.BuildAlertHTML(incident, clientCN, serialNumber, now)

	// Генерація технічного аудиторського дампу для вкладення
	attachmentContent := []byte(fmt.Sprintf(
		"=== ZERO TRUST SECURITY AUDIT LOG ===\n"+
			"Timestamp: %s\n"+
			"Verified Client CN: %s\n"+
			"Certificate Serial: %s\n"+
			"Remote Address: %s\n"+
			"Incident Details: %s\n"+
			"Action: DISPATCHED_TO_DUTY_ENGINEER\n"+
			"======================================\n",
		now.Format(time.RFC3339),
		clientCN,
		serialNumber,
		remoteAddr,
		incident,
	))

	subject := fmt.Sprintf("[CRITICAL ALERT] %s - Source: %s", incident, clientCN)
	fmt.Printf("[SMTP] Надсилання email на %s через %s:%s...\n", recipient, emailSvc.SMTPHost, emailSvc.SMTPPort)

	err = emailSvc.SendMIMEEmail([]string{recipient}, subject, htmlBody, "incident_report.log", attachmentContent)
	if err != nil {
		fmt.Printf("[SMTP] Попередження: Не вдалося доставити email через поштовий сервер (%v). Переконайтеся, що Mailpit/SMTP запущено на порту %s.\n", err, emailSvc.SMTPPort)
	} else {
		fmt.Println("[SMTP] Лист про аварію успішно доставлено до поштового шлюзу!")
	}

	// Відправка підтвердження клієнту
	responseMsg := "[SERVER]: ALERT PROCESSED AND DISPATCHED\n"
	_, _ = conn.Write([]byte(responseMsg))
	fmt.Printf("[ВІДПОВІДЬ] Клієнту надіслано статус: %s", responseMsg)
}
