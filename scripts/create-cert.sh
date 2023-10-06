#!/bin/bash

if [[ -z "$(which openssl)" ]]; then
    echo "Please install openssl"
    exit 1
fi

priv_name=$1
pub_name=$2

if [[ -z "$priv_name" ]]; then
    echo "No private key name supplied. Usage 'create-cert.sh {priv name} {pub name}'"
    exit 1
fi

if [[ -z "$pub_name" ]]; then
    echo "No public key name supplied. Usage 'create-cert.sh {priv name} {pub name}'"
    exit 1
fi

openssl genrsa -out "$priv_name"
openssl rsa -in "$priv_name" -pubout -out "$pub_name"