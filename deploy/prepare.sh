#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ "$(id -u)" = 0 ]; then
    echo "Run as a non-root user so the image and credentials share that UID." >&2
    exit 1
fi
if [ -e deploy/.env ] || [ -e deploy/pki ]; then
    echo "Refusing to replace deployment identity/configuration; use the existing setup." >&2
    exit 1
fi
go build -o bin/fencelab ./cmd/fencelab
umask 077
bin/fencelab cluster-pki -dir deploy/pki
printf 'FENCELAB_UID=%s\nFENCELAB_GID=%s\n' "$(id -u)" "$(id -g)" > deploy/.env
echo "Prepared local credentials. Keep deploy/pki/issuer offline and never mount it."
