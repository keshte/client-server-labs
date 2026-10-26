package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

func runTCPClient(address string) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		fmt.Printf("Помилка підключення до TCP сервера %s: %v\n", address, err)
		return
	}
	defer conn.Close()

	fmt.Printf("[КЛІЄНТ] Успішно підключено до %s\n", address)
	fmt.Println("[КЛІЄНТ] Введіть команду (ECHO <текст>, TIME, BROADCAST <текст>, CLOSE):")

	// Фонове читання відповідей від сервера
	go func() {
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					fmt.Println("\n[КЛІЄНТ] З'єднання розірвано сервером.")
				}
				os.Exit(0)
			}
			fmt.Print(line)
		}
	}()

	// Читання вводу користувача з консолі
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		text := scanner.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		fmt.Fprintf(conn, "%s\n", text)
		if strings.EqualFold(text, "CLOSE") || strings.EqualFold(text, "QUIT") {
			break
		}
	}
}

func runUDPSender(address, message string) {
	conn, err := net.Dial("udp", address)
	if err != nil {
		fmt.Printf("Помилка створення UDP сокета: %v\n", err)
		return
	}
	defer conn.Close()

	_, err = conn.Write([]byte(message))
	if err != nil {
		fmt.Printf("Помилка відправки UDP датаграми: %v\n", err)
		return
	}

	fmt.Printf("[UDP КЛІЄНТ] Датаграму успішно надіслано на %s: \"%s\"\n", address, message)
}

func main() {
	proto := flag.String("proto", "tcp", "Протокол: tcp або udp")
	addr := flag.String("addr", "localhost:9000", "Адреса підключення (за замовчуванням localhost:9000 для TCP або localhost:9001 для UDP)")
	msg := flag.String("msg", "temp=24.5;press=750", "Повідомлення для відправки по UDP")
	flag.Parse()

	if *proto == "udp" {
		if *addr == "localhost:9000" {
			*addr = "localhost:9001"
		}
		runUDPSender(*addr, *msg)
	} else {
		runTCPClient(*addr)
	}
}
