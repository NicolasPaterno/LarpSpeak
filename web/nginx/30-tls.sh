#!/bin/sh
# Turns on HTTPS when certificates are mounted (make certs -> web/certs/).
# Without them nginx keeps serving plain HTTP, so the stack always starts.
# Runs after the image's 20-envsubst-on-templates.sh has written default.conf.
set -eu
crt=/etc/nginx/certs/cert.pem key=/etc/nginx/certs/key.pem
if [ -f "$crt" ] && [ -f "$key" ]; then
  sed -i "s|    listen 80;|    listen 80;\n    listen 443 ssl;\n    ssl_certificate $crt;\n    ssl_certificate_key $key;|" /etc/nginx/conf.d/default.conf
  echo "30-tls.sh: HTTPS enabled on 443"
else
  echo "30-tls.sh: no certificates in /etc/nginx/certs, HTTP only"
fi
