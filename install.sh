#!/bin/sh
# Qazyna installer: downloads the latest prebuilt binary from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/izhanov/qazyna/main/install.sh | sh
#
# Override the install directory with QAZYNA_INSTALL_DIR (default ~/.local/bin).
set -eu

REPO="izhanov/qazyna"
INSTALL_DIR="${QAZYNA_INSTALL_DIR:-$HOME/.local/bin}"

os=$(uname -s)
arch=$(uname -m)

case "$os" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*)
	echo "unsupported OS: $os (build from source: https://github.com/$REPO)" >&2
	exit 1
	;;
esac

case "$arch" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*)
	echo "unsupported architecture: $arch (build from source: https://github.com/$REPO)" >&2
	exit 1
	;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" |
	sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
if [ -z "$tag" ]; then
	echo "cannot determine the latest release of $REPO" >&2
	exit 1
fi

asset="qazyna_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$tag/$asset"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "downloading qazyna $tag (${os}/${arch})..."
curl -fSL --progress-bar "$url" -o "$tmp/$asset"
tar -xzf "$tmp/$asset" -C "$tmp"

mkdir -p "$INSTALL_DIR"
install -m 0755 "$tmp/qazyna" "$INSTALL_DIR/qazyna"
echo "installed: $INSTALL_DIR/qazyna ($("$INSTALL_DIR/qazyna" --version))"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	echo
	echo "note: $INSTALL_DIR is not in your PATH; add to your shell profile:"
	echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
	;;
esac

echo
echo "next: qazyna setup"
