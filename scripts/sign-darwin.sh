#!/usr/bin/env bash
# Sign (and, in release CI, notarize) one marina binary. Called by goreleaser's
# post-build hook and by `make build`.
#
#   MARINA_SIGN_IDENTITY  Developer ID identity. Unset: ad-hoc signature, which
#                         is enough to start a VM on the machine that built it.
#   NOTARY_APPLE_ID, NOTARY_TEAM_ID, NOTARY_PASSWORD
#                         All three set: submit to Apple's notary service and
#                         wait for the verdict. Needs a Developer ID signature.
#
# Every signature carries the com.apple.security.virtualization entitlement;
# without it Virtualization.framework refuses to start a VM, with an error that
# does not mention signing.
#
# A Developer ID signature gives the binary a stable identity (team ID) that
# survives upgrades, which is what an MDM or EDR allowlist (Kandji, Falcon)
# keys on. An ad-hoc signature changes with every build.
set -euo pipefail

bin="$1"
here="$(cd "$(dirname "$0")/.." && pwd)"
entitlements="$here/entitlements.plist"

if [ -z "${MARINA_SIGN_IDENTITY:-}" ]; then
  codesign --force --sign - --entitlements "$entitlements" "$bin"
  exit 0
fi

# Notarization requires the hardened runtime and a secure timestamp. Local
# builds can pass MARINA_SIGN_TIMESTAMP=none to sign offline.
codesign --force --options runtime --timestamp${MARINA_SIGN_TIMESTAMP:+=$MARINA_SIGN_TIMESTAMP} \
  --entitlements "$entitlements" --sign "$MARINA_SIGN_IDENTITY" "$bin"
codesign --verify --strict --verbose=2 "$bin"

if [ -z "${NOTARY_APPLE_ID:-}" ] || [ -z "${NOTARY_TEAM_ID:-}" ] || [ -z "${NOTARY_PASSWORD:-}" ]; then
  exit 0
fi

# A bare Mach-O cannot be stapled: the notary service records the cdhash, and
# Gatekeeper looks the ticket up online the first time the binary runs.
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
ditto -c -k --keepParent "$bin" "$work/marina.zip"

# notarytool has no @env: syntax for --password; GitHub masks the secret.
result="$(xcrun notarytool submit "$work/marina.zip" \
  --apple-id "$NOTARY_APPLE_ID" --team-id "$NOTARY_TEAM_ID" --password "$NOTARY_PASSWORD" \
  --wait --output-format json)"
echo "$result"
status="$(/usr/bin/plutil -extract status raw - <<<"$result" 2>/dev/null || true)"
if [ "$status" != "Accepted" ]; then
  id="$(/usr/bin/plutil -extract id raw - <<<"$result" 2>/dev/null || true)"
  echo "::error::notarization of $bin returned '${status:-no status}'" >&2
  if [ -n "$id" ]; then
    xcrun notarytool log "$id" \
      --apple-id "$NOTARY_APPLE_ID" --team-id "$NOTARY_TEAM_ID" --password "$NOTARY_PASSWORD" >&2 || true
  fi
  exit 1
fi
