package email

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"net/smtp"
	"strings"
	"time"
)

// EmailService відповідає за формування та відправку листів через SMTP
type EmailService struct {
	SMTPHost string
	SMTPPort string
	Auth     smtp.Auth
	From     string
}

// NewEmailService створює новий сервіс відправки пошти
func NewEmailService(host, port, from string, auth smtp.Auth) *EmailService {
	return &EmailService{
		SMTPHost: host,
		SMTPPort: port,
		From:     from,
		Auth:     auth,
	}
}

// SendSimpleEmail відправляє базове текстове сповіщення (Рівень 2)
func (s *EmailService) SendSimpleEmail(to []string, subject, body string) error {
	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s\r\n", s.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	addr := fmt.Sprintf("%s:%s", s.SMTPHost, s.SMTPPort)
	return smtp.SendMail(addr, s.Auth, s.From, to, msg.Bytes())
}

// SendMIMEEmail відправляє багатокомпонентний лист із HTML та Base64 вкладенням (Рівень 3)
func (s *EmailService) SendMIMEEmail(to []string, subject, htmlBody, attachmentName string, attachmentData []byte) error {
	boundary := fmt.Sprintf("----=_Part_%d_%d", time.Now().UnixNano(), 123456)

	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s\r\n", s.From))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
	msg.WriteString("\r\n")

	// Секція 1: HTML-розмітка
	msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
	msg.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
	msg.WriteString("Content-Transfer-Encoding: 8bit\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(htmlBody)
	msg.WriteString("\r\n")

	// Секція 2: Вкладений файл (Base64)
	if len(attachmentData) > 0 {
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString(fmt.Sprintf("Content-Type: application/octet-stream; name=\"%s\"\r\n", attachmentName))
		msg.WriteString("Content-Transfer-Encoding: base64\r\n")
		msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", attachmentName))
		msg.WriteString("\r\n")

		encoded := base64.StdEncoding.EncodeToString(attachmentData)
		// Розбиття Base64 на рядки по 76 символів згідно зі стандартом RFC 2045
		for i := 0; i < len(encoded); i += 76 {
			end := i + 76
			if end > len(encoded) {
				end = len(encoded)
			}
			msg.WriteString(encoded[i:end] + "\r\n")
		}
	}

	// Фінальний розділювач
	msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))

	addr := fmt.Sprintf("%s:%s", s.SMTPHost, s.SMTPPort)
	return smtp.SendMail(addr, s.Auth, s.From, to, msg.Bytes())
}

// BuildAlertHTML формує структурований HTML звіт тривоги
func BuildAlertHTML(incident, clientCN, serialNumber string, eventTime time.Time) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif; background-color: #f6f8fa; padding: 20px; }
.card { background-color: #ffffff; border: 1px solid #d0d7de; border-radius: 6px; padding: 24px; max-width: 600px; margin: 0 auto; box-shadow: 0 1px 3px rgba(0,0,0,0.1); }
.header { border-bottom: 2px solid #cf222e; padding-bottom: 12px; margin-bottom: 16px; }
.badge { background-color: #ffebe9; color: #cf222e; border: 1px solid #ff8182; padding: 4px 8px; border-radius: 4px; font-weight: bold; font-size: 12px; text-transform: uppercase; }
table { width: 100%%; border-collapse: collapse; margin-top: 16px; font-size: 14px; }
th, td { text-align: left; padding: 8px 12px; border-bottom: 1px solid #e1e4e8; }
th { background-color: #f6f8fa; width: 35%%; color: #57606a; }
.log { background-color: #24292f; color: #f0f6fc; padding: 12px; border-radius: 4px; font-family: monospace; font-size: 13px; margin-top: 16px; overflow-x: auto; }
</style>
</head>
<body>
<div class="card">
<div class="header">
<span class="badge">Критична тривога (Zero Trust Alert)</span>
<h2 style="margin: 8px 0 0 0; color: #24292f;">Secure Alert Gateway Notification</h2>
</div>
<table>
<tr><th>Час події</th><td>%s</td></tr>
<tr><th>Автентифікація</th><td>mTLS (Mutual TLS X.509)</td></tr>
<tr><th>Джерело (CN)</th><td><strong>%s</strong></td></tr>
<tr><th>Серійний номер</th><td><code>%s</code></td></tr>
</table>
<h4 style="margin: 16px 0 4px 0; color: #24292f;">Повідомлення про інцидент:</h4>
<div class="log">%s</div>
<p style="font-size: 12px; color: #57606a; margin-top: 20px;">
Детальний дамп аудиту додано у вкладенні incident_report.log.
</p>
</div>
</body>
</html>`,
		eventTime.Format(time.RFC1123),
		clientCN,
		serialNumber,
		incident,
	)
}
