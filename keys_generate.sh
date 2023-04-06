openssl genrsa -out private.key 4096
openssl req -x509 -new -nodes -days 365 -key private.key -out ca.crt
