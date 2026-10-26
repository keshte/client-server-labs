package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"securegateway/email"
)

func getTestCerts(t *testing.T) (tls.Certificate, tls.Certificate, *x509.CertPool) {
	certsDir := "certs"
	if _, err := os.Stat(filepath.Join(certsDir, "ca.crt")); err != nil {
		certsDir = filepath.Join("..", "certs")
	}

	serverCert, err := tls.LoadX509KeyPair(filepath.Join(certsDir, "server.crt"), filepath.Join(certsDir, "server.key"))
	if err != nil {
		t.Fatalf("failed to load server cert: %v", err)
	}

	clientCert, err := tls.LoadX509KeyPair(filepath.Join(certsDir, "client.crt"), filepath.Join(certsDir, "client.key"))
	if err != nil {
		t.Fatalf("failed to load client cert: %v", err)
	}

	caPEM, err := os.ReadFile(filepath.Join(certsDir, "ca.crt"))
	if err != nil {
		t.Fatalf("failed to read ca cert: %v", err)
	}

	caPool := x509.NewCertPool()
	caPool.AppendCertsFromPEM(caPEM)

	return serverCert, clientCert, caPool
}

// startMockSMTPServer створює тестовий SMTP-сервер у пам'яті
func startMockSMTPServer(t *testing.T) (string, chan string) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock smtp listener: %v", err)
	}

	port := fmt.Sprintf("%d", l.Addr().(*net.TCPAddr).Port)
	receivedData := make(chan string, 1)

	go func() {
		defer l.Close()
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		fmt.Fprintf(conn, "220 mock.smtp.local Simple Mail Service\r\n")

		scanner := bufio.NewScanner(conn)
		var fullData strings.Builder
		inData := false

		for scanner.Scan() {
			line := scanner.Text()
			if inData {
				if line == "." {
					inData = false
					fmt.Fprintf(conn, "250 2.0.0 Ok: queued\r\n")
					receivedData <- fullData.String()
				} else {
					fullData.WriteString(line + "\n")
				}
				continue
			}

			upper := strings.ToUpper(line)
			if strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO") {
				fmt.Fprintf(conn, "250-mock.smtp.local\r\n250 HELP\r\n")
			} else if strings.HasPrefix(upper, "MAIL FROM:") {
				fmt.Fprintf(conn, "250 2.1.0 Ok\r\n")
			} else if strings.HasPrefix(upper, "RCPT TO:") {
				fmt.Fprintf(conn, "250 2.1.5 Ok\r\n")
			} else if strings.HasPrefix(upper, "DATA") {
				fmt.Fprintf(conn, "354 End data with <CR><LF>.<CR><LF>\r\n")
				inData = true
			} else if strings.HasPrefix(upper, "QUIT") {
				fmt.Fprintf(conn, "221 2.0.0 Bye\r\n")
				break
			}
		}
	}()

	return port, receivedData
}

func TestAuthorizedMTLSConnection(t *testing.T) {
	serverCert, clientCert, caPool := getTestCerts(t)

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS13,
	}

	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("failed to start tls listener: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()

	// Горутина обробки сервера
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			t.Errorf("server handshake failed: %v", err)
			return
		}

		state := tlsConn.ConnectionState()
		if len(state.PeerCertificates) == 0 {
			t.Errorf("no peer certificates on server")
			return
		}

		clientCN := state.PeerCertificates[0].Subject.CommonName
		if clientCN != "dispatcher-agent-01" {
			t.Errorf("expected CN=dispatcher-agent-01, got %s", clientCN)
		}

		reader := bufio.NewReader(conn)
		msg, _ := reader.ReadString('\n')
		if !strings.Contains(msg, "CRITICAL_ERROR") {
			t.Errorf("unexpected message: %s", msg)
		}

		fmt.Fprintln(conn, "[SERVER]: ALERT PROCESSED AND DISPATCHED")
	}()

	// Клієнт
	clientTLS := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
		ServerName:   "localhost",
	}

	clientConn, err := tls.Dial("tcp", serverAddr, clientTLS)
	if err != nil {
		t.Fatalf("client failed to connect with mTLS: %v", err)
	}
	defer clientConn.Close()

	fmt.Fprintf(clientConn, "CRITICAL_ERROR: Database connection timeout on node-04\n")

	resp, err := bufio.NewReader(clientConn).ReadString('\n')
	if err != nil {
		t.Fatalf("failed to read server response: %v", err)
	}

	if !strings.Contains(resp, "ALERT PROCESSED AND DISPATCHED") {
		t.Errorf("unexpected server response: %s", resp)
	}

	<-serverDone
}

func TestUnauthorizedClientRejected(t *testing.T) {
	serverCert, _, caPool := getTestCerts(t)

	serverTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    caPool,
		MinVersion:   tls.VersionTLS13,
	}

	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatalf("failed to start tls listener: %v", err)
	}
	defer listener.Close()

	serverAddr := listener.Addr().String()

	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			_ = conn.(*tls.Conn).Handshake()
		}
	}()

	// Підключення БЕЗ клієнтського сертифіката
	clientTLS := &tls.Config{
		RootCAs:    caPool,
		ServerName: "localhost",
	}

	clientConn, err := tls.Dial("tcp", serverAddr, clientTLS)
	if err == nil {
		defer clientConn.Close()
		// Спроба відправки або вичитування обов'язково призведе до помилки TLS через відсутність сертифіката
		_, _ = clientConn.Write([]byte("TEST\n"))
		buf := make([]byte, 128)
		_, err = clientConn.Read(buf)
		if err == nil {
			t.Fatal("expected connection to fail without client certificate, but it succeeded")
		}
	}
}

func TestMIMEEmailDelivery(t *testing.T) {
	smtpPort, receivedChan := startMockSMTPServer(t)

	emailSvc := email.NewEmailService("127.0.0.1", smtpPort, "gateway@internal.net", nil)
	to := []string{"duty-engineer@internal.net"}
	subject := "Test Alert"
	htmlBody := email.BuildAlertHTML("CRITICAL_ERROR: Test Alert", "dispatcher-agent-01", "12345", time.Now())
	attachment := []byte("Audit log test dump content")

	err := emailSvc.SendMIMEEmail(to, subject, htmlBody, "incident_report.log", attachment)
	if err != nil {
		t.Fatalf("failed to send mime email: %v", err)
	}

	select {
	case data := <-receivedChan:
		if !strings.Contains(data, "multipart/mixed") {
			t.Errorf("missing multipart/mixed header in email: %s", data)
		}
		if !strings.Contains(data, "incident_report.log") {
			t.Errorf("missing attachment header in email: %s", data)
		}
		if !strings.Contains(data, "dispatcher-agent-01") {
			t.Errorf("missing client CN in email body: %s", data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for email delivery to mock smtp")
	}
}
