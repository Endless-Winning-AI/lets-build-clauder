#!/bin/sh
# Builds Let's Build Clauder, signs it, installs the daemon binary and sets up the
# LaunchAgent that keeps it running. Safe to re-run after a code change.
#
#   scripts/install-macos.sh [--identity "Apple Development: you@example.com (TEAMID)"]
#
# Signing with a stable identity is what lets macOS keep the Input Monitoring
# and Accessibility grants across rebuilds. Without --identity the first
# "Apple Development" identity in your keychain is used; with none, the build
# is ad-hoc signed and the grants must be re-added after each rebuild.
set -eu

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
label=ai.endlesswinning.lets-build-clauder
app_support="$HOME/Library/Application Support/lets-build-clauder"
target="$app_support/lets-build-clauder"
plist="$HOME/Library/LaunchAgents/$label.plist"
logdir="$HOME/Library/Logs/lets-build-clauder"
identity=""

while [ $# -gt 0 ]; do
	case $1 in
		--identity) identity=$2; shift 2 ;;
		-h | --help) sed -n '2,11p' "$0"; exit 0 ;;
		*) echo "install-macos.sh: unknown option $1" >&2; exit 2 ;;
	esac
done

[ "$(uname -s)" = Darwin ] || { echo "install-macos.sh: macOS only" >&2; exit 1; }
command -v go >/dev/null || { echo "install-macos.sh: Go is needed (brew install go)" >&2; exit 1; }

if [ -z "$identity" ]; then
	identity=$(security find-identity -v -p codesigning 2>/dev/null \
		| sed -n 's/.*"\(Apple Development: [^"]*\)".*/\1/p' | head -n 1 || true)
fi
if [ -z "$identity" ]; then
	echo "install-macos.sh: no Apple Development identity found; signing ad-hoc" >&2
	identity="-"
fi

echo "building"
(cd "$root" && go build -o build/lets-build-clauder ./cmd/lets-build-clauder)

echo "signing as: $identity"
staged=$(mktemp -t lets-build-clauder)
cp "$root/build/lets-build-clauder" "$staged"
codesign -f -s "$identity" -i "$label" -o runtime "$staged"

echo "installing $target"
mkdir -p "$app_support" "$logdir" "$HOME/Library/LaunchAgents"
# Replace the file whole so a running daemon keeps its old mapping until restarted.
mv -f "$staged" "$target"
chmod 755 "$target"

echo "writing $plist"
cat > "$plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$label</string>
	<key>ProgramArguments</key>
	<array>
		<string>$target</string>
		<string>daemon</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Interactive</string>
	<key>StandardOutPath</key>
	<string>$logdir/daemon.log</string>
	<key>StandardErrorPath</key>
	<string>$logdir/daemon.log</string>
</dict>
</plist>
PLIST

domain=gui/$(id -u)
echo "loading $label"
launchctl bootout "$domain/$label" 2>/dev/null || true
# bootout returns before the service is gone, and bootstrap fails until it is.
for _ in 1 2 3 4 5 6 7 8 9 10; do
	launchctl print "$domain/$label" >/dev/null 2>&1 || break
	sleep 0.5
done
# A session may have started its own copy while none ran; it lacks the
# agent's macOS grants, so it must not keep the socket.
pkill -f "$target daemon" 2>/dev/null || true
launchctl bootstrap "$domain" "$plist"
launchctl kickstart -k "$domain/$label"

cat <<MSG

Installed. Let's Build Clauder's daemon now starts at login.

If this is a new install or a new signing identity, macOS will block the pad
until you allow this program under both
  System Settings > Privacy & Security > Input Monitoring
  System Settings > Privacy & Security > Accessibility
Add it with +, press Cmd-Shift-G and paste:
  $target
Then run:  launchctl kickstart -k $domain/$label
Check with: "$target" status
MSG
