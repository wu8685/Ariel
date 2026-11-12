#!/bin/sh
# Headless Puck acceptance for #93 and #94.
# Uses a new temp directory, a temp owner session, and the local Codex app-server.
# It does not read ArielLocal, Passkey credentials, deployment tokens, or ~/.codex/config.toml.
set -eu
cd "$(dirname "$0")/../.."
unset ARIEL_TOKEN ARIEL_WEB_PIN ARIEL_PUCK_DATA_DIR ARIEL_PUCK_ROOT ARIEL_LISTEN ARIEL_WEBAUTHN_CREDENTIALS_FILE ARIEL_SESSION_KEY ARIEL_PASSKEY_SETUP_TOKEN
exec go run ./cmd/puck-e2e "$@"
