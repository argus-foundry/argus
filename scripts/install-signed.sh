#!/bin/sh
# Build argus, self-sign it with a stable code-signing identity (when present),
# and install it ATOMICALLY. Driven by `make install-signed`.
#
# Guarantees:
#   * The live binary is never half-deployed: we build + sign a temp file next
#     to the destination and only `mv` it into place once signing succeeded.
#     Any failure leaves the previously-installed binary untouched.
#   * Never hangs on a locked login keychain / pending GUI "Always Allow"
#     prompt: a non-interactive preflight fails fast, and codesign itself runs
#     under a hard timeout as a backstop. Failure is a non-zero exit with a
#     clear stderr line (so iris reports it instead of timing out).
#
# No identity installed => plain unsigned install (unchanged opt-in behaviour).
#
# Env:
#   ARGUS_SIGN_IDENTITY  identity name to look up   (default "Argus Code Signing")
#   ARGUS_SIGN_TIMEOUT   seconds for codesign/security probes (default 30)
#   ARGUS_CODESIGN_TIMEOUT  seconds for codesign alone (default: ARGUS_SIGN_TIMEOUT)
#   ARGUS_SKIP_SIGN=1    deliberately install unsigned (escape hatch)
#   ARGUS_INSTALL_BIN    destination path (default $(go env GOBIN|GOPATH/bin)/argus)
#   ARGUS_BUILD_CMD      test seam: command run as `$ARGUS_BUILD_CMD <out-path>`
set -u

IDENT_NAME="${ARGUS_SIGN_IDENTITY:-Argus Code Signing}"
TIMEOUT="${ARGUS_SIGN_TIMEOUT:-30}"
CS_TIMEOUT="${ARGUS_CODESIGN_TIMEOUT:-$TIMEOUT}"
TAG="[install-signed]"

die() { echo "$TAG ERROR: $*" >&2; exit 1; }

# run_timeout SECS CMD... — run CMD, SIGALRM-kill it after SECS. Returns 124 on
# timeout. (macOS ships no `timeout`.) The alarm is armed inside the child
# itself (perl `alarm` survives `exec`), so there is NO separate watchdog
# process: the previous background `sleep`-watchdog design could outlive the
# command, and `wait`ing on it stalled an instantly-finished command for the
# full TIMEOUT+2s and then misreported it as a timeout (exit 124) — seen on CI
# as a 62s `security show-keychain-info` "locked keychain" failure.
# $cpid is the (exec'd) command pid so cleanup() can kill it on a signal.
run_timeout() {
	secs=$1; shift
	perl -e 'alarm shift; exec @ARGV or exit 127' "$secs" "$@" &
	cpid=$!
	wait "$cpid"
	rc=$?
	cpid=
	[ "$rc" -eq 142 ] && return 124 # 128 + SIGALRM
	return $rc
}

if [ -n "${ARGUS_INSTALL_BIN:-}" ]; then
	dest=$ARGUS_INSTALL_BIN
else
	bin=$(go env GOBIN); [ -n "$bin" ] || bin="$(go env GOPATH)/bin"
	dest="$bin/argus"
fi
destdir=$(dirname "$dest")
mkdir -p "$destdir" || die "cannot create $destdir"

tmp="$dest.new.$$"
# Remove the temp (and any codesign sidecar like $tmp.cstemp) on EVERY exit
# path: die(), codesign timeout, or a signal. INT/TERM must `exit` — a trap
# that only cleans would let the script resume after the signal.
cleanup() {
	[ -n "${cpid:-}" ] && kill "$cpid" 2>/dev/null
	rm -f "$tmp" "$tmp".*
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# 1. Build to a temp path (same dir => same filesystem => atomic rename).
if [ -n "${ARGUS_BUILD_CMD:-}" ]; then
	$ARGUS_BUILD_CMD "$tmp" || die "build failed"
else
	go build -o "$tmp" ./cmd/argus || die "go build failed"
fi

install_tmp() {
	mv -f "$tmp" "$dest" || die "could not move new binary into place at $dest"
}

# 2. Decide whether to sign.
if [ "${ARGUS_SKIP_SIGN:-}" = "1" ]; then
	install_tmp
	echo "$TAG ARGUS_SKIP_SIGN=1 — installed $dest UNSIGNED (macOS may re-prompt for file access)"
	exit 0
fi

# run_timeout needs perl. Check at top level: die() inside the $(...) below
# would only exit the substitution and silently fall through to "no identity".
command -v perl >/dev/null 2>&1 || die "perl is required to bound codesign/security with a timeout, and was not found on PATH (ARGUS_SKIP_SIGN=1 installs unsigned without it)"

out=$(run_timeout "$TIMEOUT" security find-identity -p codesigning 2>/dev/null)
id=$(printf '%s\n' "$out" | grep -F "$IDENT_NAME" | head -1 | awk '{print $2}')
if [ -z "$id" ]; then
	install_tmp
	echo "$TAG no '$IDENT_NAME' code-signing identity found — installed $dest ad-hoc (macOS may re-prompt for file access). To opt in, see the install-signed comment in the Makefile."
	exit 0
fi

# 3. Preflight: is the keychain usable without a human? A locked keychain
#    makes codesign block on an unanswerable GUI prompt.
run_timeout "$TIMEOUT" security show-keychain-info >/dev/null 2>&1
rc=$?
if [ "$rc" -ne 0 ]; then
	die "login keychain is locked or unavailable (security show-keychain-info exit $rc), so signing with '$IDENT_NAME' would hang. Unlock it (\`security unlock-keychain ~/Library/Keychains/login.keychain-db\` or unlock the Mac) and re-run; the live binary $dest was NOT modified. To deliberately install unsigned: ARGUS_SKIP_SIGN=1 make install-signed"
fi

# 4. Sign the TEMP file under a hard timeout.
run_timeout "$CS_TIMEOUT" codesign --force --identifier com.drn.argus --sign "$id" "$tmp"
rc=$?
if [ "$rc" -eq 124 ]; then
	die "codesign timed out after ${CS_TIMEOUT}s waiting on Keychain access for '$IDENT_NAME' — unlock the Mac / click \"Always Allow\" on the Keychain prompt and re-run; the live binary $dest was NOT modified"
elif [ "$rc" -ne 0 ]; then
	die "codesign failed (exit $rc) for '$IDENT_NAME'; the live binary $dest was NOT modified"
fi

install_tmp
echo "$TAG signed $dest with stable identity '$IDENT_NAME' ($id) — TCC grant will persist"
