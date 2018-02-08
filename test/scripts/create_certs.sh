#!/bin/sh

openssl genrsa -out server.key 1024
openssl req -new -key server.key -out server.csr << CERT_END
US
US
Nowhere
Cb-Event-Forwarder Test

localhost
nobody@localhost


CERT_END
openssl x509 -req -days 365 -in server.csr -signkey server.key -out server.crt
