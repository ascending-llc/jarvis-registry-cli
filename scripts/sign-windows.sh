#!/usr/bin/env bash
set -euo pipefail

# Signs a Windows release binary in place with Azure Artifact Signing via jsign.
# Invoked by GoReleaser as a post-build hook once per target:
#   scripts/sign-windows.sh <goos> <binary path>
# Non-Windows targets are a no-op.

readonly keystore="eus.codesigning.azure.net"
readonly alias_name="ascending-jarvis-signing/jarvis-public-trust"
readonly tsa_url="http://timestamp.acs.microsoft.com/"

fail() {
    printf 'sign-windows: %s\n' "$1" >&2
    exit 1
}

main() {
    [[ $# -eq 2 ]] || fail "usage: sign-windows.sh <goos> <binary path>"

    local goos="$1"
    local artifact="$2"

    [[ "$goos" == "windows" ]] || exit 0

    [[ -n "${JSIGN_JAR:-}" ]] || fail "JSIGN_JAR is not set; refusing to ship an unsigned binary"
    [[ -n "${AZURE_CODESIGNING_TOKEN:-}" ]] ||
        fail "AZURE_CODESIGNING_TOKEN is not set; refusing to ship an unsigned binary"
    [[ -f "$JSIGN_JAR" ]] || fail "JSIGN_JAR does not point to a file: $JSIGN_JAR"
    [[ -f "$artifact" ]] || fail "binary not found: $artifact"

    # The token is read by jsign from the environment (env:), never placed on a command line.
    java -jar "$JSIGN_JAR" \
        --storetype TRUSTEDSIGNING \
        --keystore "$keystore" \
        --storepass env:AZURE_CODESIGNING_TOKEN \
        --alias "$alias_name" \
        --tsaurl "$tsa_url" \
        --tsmode RFC3161 \
        --name "Jarvis Registry CLI" \
        --url https://jarvisregistry.com/ \
        "$artifact"
}

main "$@"
