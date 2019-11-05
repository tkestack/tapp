#!/bin/bash

set -e
set -x

if [ $# -ne 2 ]; then
    echo "Usage: $0 $key_dir $namespace"
    echo "    key_dir: used to put certs in"
    echo "    namespace: the namespace that webhook-server will be deployed in"
    exit 1
fi

key_dir="$1"
namespace="$2"

mkdir -p $key_dir
chmod 0700 $key_dir
cd $key_dir

# Generate the CA cert and private key
openssl req -nodes -new -x509 -days 100000 -keyout ca.key -out ca.crt -subj "/CN=Admission Webhook Server CA"
# Generate the private key for the webhook server
openssl genrsa -out tls.key 2048
# Generate a Certificate Signing Request (CSR) for the private key, and sign it with the private key of the CA.
openssl req -new -days 100000 -key tls.key -subj "/CN=tapp-controller.kube-system.svc" \
    | openssl x509 -req -days 100000 -CA ca.crt -CAkey ca.key -CAcreateserial -out tls.crt
