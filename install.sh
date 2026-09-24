#!/bin/sh
# install.sh - download and install a rest-o-matic release from GitHub.
#
#   curl -fsSL https://raw.githubusercontent.com/drewlsvern/rest-o-matic/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- --version v0.0.1-rc.3
#
# Plain POSIX sh, so it runs on distros without bash (Alpine, busybox).
# Needs curl or wget, tar, and sha256sum or shasum. Run with --help for
# options.

set -eu

REPO="drewlsvern/rest-o-matic"
API="https://api.github.com/repos/$REPO"
DOWNLOAD="https://github.com/$REPO/releases/download"

usage() {
	cat <<EOF
Install rest-o-matic from its GitHub releases.

Usage: install.sh [options]

  -v, --version TAG  install this release (e.g. v0.0.1-rc.3) instead of the
                     newest one, pre-releases included
  -l, --list         list available releases, newest first, and exit
  -d, --dir DIR      install into DIR (default: /usr/local/bin, or
                     ~/.local/bin when that isn't writable and sudo isn't
                     available)
  -h, --help         show this help

The environment variables RESTOMATIC_VERSION and RESTOMATIC_INSTALL_DIR set
the same defaults as --version and --dir.
EOF
}

say() { printf '%s\n' "$*"; }
die() {
	printf 'install.sh: %s\n' "$*" >&2
	exit 1
}

version="${RESTOMATIC_VERSION:-latest}"
install_dir="${RESTOMATIC_INSTALL_DIR:-}"
list=0

while [ $# -gt 0 ]; do
	case $1 in
	-v | --version)
		[ $# -ge 2 ] || die "$1 needs a value"
		version=$2
		shift 2
		;;
	--version=*)
		version=${1#*=}
		shift
		;;
	-d | --dir)
		[ $# -ge 2 ] || die "$1 needs a value"
		install_dir=$2
		shift 2
		;;
	--dir=*)
		install_dir=${1#*=}
		shift
		;;
	-l | --list)
		list=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*) die "unknown option: $1 (see --help)" ;;
	esac
done

# fetch URL [FILE]: download URL to FILE, or to stdout without one.
if command -v curl >/dev/null 2>&1; then
	fetch() {
		if [ $# -ge 2 ]; then curl -fsSL -o "$2" "$1"; else curl -fsSL "$1"; fi
	}
elif command -v wget >/dev/null 2>&1; then
	fetch() {
		if [ $# -ge 2 ]; then wget -q -O "$2" "$1"; else wget -q -O - "$1"; fi
	}
else
	die "curl or wget is required"
fi

# releases: print "TAG release" or "TAG pre-release" per published
# release, newest first. The GitHub API's JSON is split on commas and read
# with awk, so jq isn't needed; each release's tag_name comes before its
# prerelease field.
releases() {
	fetch "$API/releases?per_page=100" | tr ',' '\n' | awk '
		/"tag_name":/ { sub(/.*"tag_name": *"/, ""); sub(/".*/, ""); tag = $0 }
		/"prerelease":/ && tag != "" {
			print tag, (/true/ ? "pre-release" : "release")
			tag = ""
		}'
}

if [ "$list" = 1 ]; then
	all=$(releases) || die "could not list releases from GitHub"
	[ -n "$all" ] || die "no releases found"
	say "$all"
	exit 0
fi

if [ "$version" = latest ]; then
	all=$(releases) || die "could not list releases from GitHub (try --version TAG)"
	version=$(say "$all" | awk 'NR == 1 { print $1 }')
	[ -n "$version" ] || die "no releases found"
fi
case $version in
v*) ;;
*) version="v$version" ;;
esac

case $(uname -s) in
Linux) os=linux ;;
Darwin) os=darwin ;;
*) die "unsupported OS $(uname -s); on Windows, download the .zip from https://github.com/$REPO/releases" ;;
esac
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture $(uname -m); releases are built for amd64 and arm64" ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
	sha256() { sha256sum -c "$1"; }
elif command -v shasum >/dev/null 2>&1; then
	sha256() { shasum -a 256 -c "$1"; }
else
	die "sha256sum or shasum is required to verify the download"
fi

asset="rest-o-matic_${version#v}_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 130' INT TERM

say "Downloading rest-o-matic $version ($os/$arch)..."
fetch "$DOWNLOAD/$version/$asset" "$tmp/$asset" ||
	die "could not download $asset for $version; check the version with --list"
fetch "$DOWNLOAD/$version/checksums.txt" "$tmp/checksums.txt" ||
	die "could not download checksums.txt for $version"

grep " $asset\$" "$tmp/checksums.txt" >"$tmp/sum" || die "$asset is missing from checksums.txt"
(cd "$tmp" && sha256 sum >/dev/null) || die "checksum mismatch for $asset"
tar -xzf "$tmp/$asset" -C "$tmp" rest-o-matic || die "could not extract $asset"

# Pick the install directory, and whether writing to it needs sudo.
sudo=""
if [ -z "$install_dir" ]; then
	install_dir=/usr/local/bin
	if [ "$(id -u)" != 0 ] && ! [ -w "$install_dir" ] && ! command -v sudo >/dev/null 2>&1; then
		install_dir="$HOME/.local/bin"
	fi
fi
if [ "$(id -u)" != 0 ]; then
	# The nearest existing directory decides whether mkdir -p needs sudo too.
	probe=$install_dir
	while ! [ -d "$probe" ]; do probe=$(dirname "$probe"); done
	if ! [ -w "$probe" ] || { [ -e "$install_dir" ] && ! [ -w "$install_dir" ]; }; then
		command -v sudo >/dev/null 2>&1 || die "$install_dir is not writable; rerun as root or pass --dir"
		sudo=sudo
	fi
fi

$sudo mkdir -p "$install_dir"
$sudo install -m 0755 "$tmp/rest-o-matic" "$install_dir/rest-o-matic"
say "Installed $("$install_dir/rest-o-matic" --version | head -n 1) to $install_dir/rest-o-matic"

case ":$PATH:" in
*":$install_dir:"*) ;;
*) say "Note: $install_dir is not on your PATH; add it, or run $install_dir/rest-o-matic." ;;
esac
command -v restic >/dev/null 2>&1 ||
	say "Note: restic was not found on PATH; rest-o-matic needs it (https://restic.net)."
