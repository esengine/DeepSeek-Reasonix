To generate a valid certificate and private key use the following command:

```bash
# In OpenSSL ≥ 1.1.1
openssl req -x509 -newkey rsa:4096 -sha256 -days 9999 -nodes \
  -keyout key.pem -out cert.pem -subj "/CN=localhost" \
  -addext "subjectAltName=DNS:localhost,IP:127.0.0.1,IP:::1"
```

To check a certificate use the following command:
```bash
openssl x509 -in cert.pem -text
```
