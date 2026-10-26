package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
	serverAddr := flag.String("addr", "127.0.0.1:8443", "Адреса mTLS-сервера")
	alertMsg := flag.String("msg", "CRITICAL_ERROR: Database connection timeout on node-04", "Текст аварійного сповіщення")
	noCert := flag.Bool("no-cert", false, "Спробувати підключитися без клієнтського сертифіката (тест відхилення)")
	flag.Parse()

	certsDir := findCertsDir()
	caFile := filepath.Join(certsDir, "ca.crt")
	clientCertFile := filepath.Join(certsDir, "client.crt")
	clientKeyFile := filepath.Join(certsDir, "client.key")

	fmt.Println("=== Агент диспетчеризації (mTLS Client) ===")

	// 1. Завантаження Root CA для перевірки сервера
	caCertPEM, err := os.ReadFile(caFile)
	if err != nil {
		fmt.Printf("[ПОМИЛКА] Не вдалося прочитати Root CA (%s): %v\n", caFile, err)
		os.Exit(1)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		fmt.Println("[ПОМИЛКА] Не вдалося додати ca.crt у пул кореневих сертифікатів")
		os.Exit(1)
	}

	clientTLSConfig := &tls.Config{
		RootCAs:            caPool,
		ServerName:         "localhost",
		InsecureSkipVerify: false,
	}

	// 2. Якщо прапорець -no-cert не задано, додаємо клієнтський сертифікат
	if !*noCert {
		clientCert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
		if err != nil {
			fmt.Printf("[ПОМИЛКА] Не вдалося завантажити клієнтський сертифікат: %v\n", err)
			os.Exit(1)
		}
		clientTLSConfig.Certificates = []tls.Certificate{clientCert}
		fmt.Printf("[КЛІЄНТ] Завантажено сертифікат клієнта: %s (CN=dispatcher-agent-01)\n", filepath.Base(clientCertFile))
	} else {
		fmt.Println("[КЛІЄНТ] УВАГА: Запуск без клієнтського сертифіката (тестування політики безпеки)!")
	}

	// 3. Підключення через tls.Dial
	fmt.Printf("[КЛІЄНТ] Підключення до шлюзу %s...\n", *serverAddr)
	conn, err := tls.Dial("tcp", *serverAddr, clientTLSConfig)
	if err != nil {
		fmt.Printf("[ВІДХИЛЕНО] Помилка TLS з'єднання від сервера: %v\n", err)
		return
	}
	defer conn.Close()

	state := conn.ConnectionState()
	protoStr := "TLS 1.3"
	if state.Version == tls.VersionTLS12 {
		protoStr = "TLS 1.2"
	}
	fmt.Printf("[КЛІЄНТ] mTLS-рукостискання успішне. Використано протокол %s\n", protoStr)

	// 4. Відправка алерту
	fmt.Printf("[КЛІЄНТ] Відправка алерту: %s\n", *alertMsg)
	fmt.Fprintf(conn, "%s\n", *alertMsg)

	// 5. Отримання відповіді від сервера
	reader := bufio.NewReader(conn)
	resp, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("[ПОМИЛКА] Не вдалося прочитати відповідь: %v\n", err)
		return
	}

	fmt.Printf("[КЛІЄНТ] Відповідь від сервера: %s", resp)
	if strings.Contains(resp, "ALERT PROCESSED") || strings.Contains(resp, "ALERT RECEIVED") {
		fmt.Println("[КЛІЄНТ] Сесію успішно завершено.")
	}
}
