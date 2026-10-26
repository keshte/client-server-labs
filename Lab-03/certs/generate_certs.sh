#!/usr/bin/env bash
set -e

cd "$(dirname "$0")"

echo "1. Генерація кореневого центру сертифікації (Root CA)..."
openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt -days 365 -subj "/CN=MyRootCA"

echo "2. Генерація сертифіката сервера..."
openssl req -newkey rsa:2048 -nodes -keyout server.key -out server.csr -subj "/CN=localhost"
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 \
  -extfile <(echo "subjectAltName=DNS:localhost,IP:127.0.0.1")

echo "3. Генерація клієнтського сертифіката..."
openssl req -newkey rsa:2048 -nodes -keyout client.key -out client.csr -subj "/CN=dispatcher-agent-01"
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -days 365

rm -f *.csr
echo "Сертифікати успішно згенеровано!"
