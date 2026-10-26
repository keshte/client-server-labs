# Скрипт генерації PKI сертифікатів для Windows (PowerShell)
$ErrorActionPreference = "Stop"

# Визначення openssl (з системи або Git)
$openssl = "openssl"
if (-not (Get-Command "openssl" -ErrorAction SilentlyContinue)) {
    if (Test-Path "C:\Program Files\Git\usr\bin\openssl.exe") {
        $openssl = "C:\Program Files\Git\usr\bin\openssl.exe"
    } else {
        Write-Error "Утиліту openssl не знайдено в PATH та в Git. Будь ласка, встановіть openssl."
    }
}

$env:MSYS_NO_PATHCONV = "1"
$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $scriptDir

Write-Host "1. Генерація кореневого центру сертифікації (Root CA)..."
& $openssl req -x509 -newkey rsa:2048 -nodes -keyout ca.key -out ca.crt -days 365 -subj "/CN=MyRootCA"

Write-Host "2. Генерація сертифіката сервера (SAN: localhost, 127.0.0.1)..."
& $openssl req -newkey rsa:2048 -nodes -keyout server.key -out server.csr -subj "/CN=localhost"

@"
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names

[alt_names]
DNS.1 = localhost
IP.1 = 127.0.0.1
"@ | Out-File -FilePath server_ext.cnf -Encoding ascii

& $openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -extfile server_ext.cnf

Write-Host "3. Генерація клієнтського сертифіката (CN=dispatcher-agent-01)..."
& $openssl req -newkey rsa:2048 -nodes -keyout client.key -out client.csr -subj "/CN=dispatcher-agent-01"
& $openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt -days 365

# Очищення тимчасових файлів CSR
Remove-Item -Path *.csr, server_ext.cnf -ErrorAction SilentlyContinue

Write-Host "Сертифікати успішно згенеровано!"
