#!/bin/bash
set -e

CERTS_DIR="$(dirname "$0")/../certs"
mkdir -p "$CERTS_DIR"
cd "$CERTS_DIR"

# Configuration
DAYS_VALID=3650
KEY_SIZE=2048

echo "Generating CA certificate..."
openssl genrsa -out ca.key $KEY_SIZE 2>/dev/null
openssl req -new -x509 -days $DAYS_VALID -key ca.key -out ca.crt \
    -subj "/C=US/ST=Test/L=Test/O=Test/CN=PostgreSQL-Test-CA"

echo "Generating server certificate..."
openssl genrsa -out server.key $KEY_SIZE 2>/dev/null
openssl req -new -key server.key -out server.csr \
    -subj "/C=US/ST=Test/L=Test/O=Test/CN=postgres"

# Create extensions file for SAN (Subject Alternative Names)
cat > server.ext << EOF
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = postgres
DNS.2 = localhost
IP.1 = 127.0.0.1
EOF

openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
    -out server.crt -days $DAYS_VALID -extfile server.ext 2>/dev/null

# Cleanup temporary files
rm -f server.csr server.ext ca.srl

# Set permissions (PostgreSQL requires specific permissions on key)
chmod 644 ca.crt server.crt
chmod 600 ca.key server.key

echo "Certificates generated successfully"
ls -la
