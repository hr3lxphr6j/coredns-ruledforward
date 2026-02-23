#!/bin/sh
# Build OpenWrt .ipk from staging directory.
# Usage: build-ipk.sh <staging_dir> <version> <arch> <output_dir>
# Example: build-ipk.sh .build/pack/openwrt-linux-amd64 1.0.0 amd64 dist

set -e
STAGING="${1:?usage: build-ipk.sh <staging_dir> <version> <arch> <output_dir>}"
VERSION="${2:?version required}"
ARCH="${3:?arch required}"
OUTDIR="${4:?output_dir required}"

# opkg expects version to start with digit; strip leading 'v'
PKG_VERSION="$(echo "$VERSION" | sed 's/^v//')"

# Map our binary arch to OpenWrt/opkg Architecture (must match router's arch)
case "$ARCH" in
  amd64)  PKG_ARCH=x86_64 ;;
  arm64)  PKG_ARCH=aarch64 ;;
  386)    PKG_ARCH=i386 ;;
  arm6)   PKG_ARCH=arm_arm926ej-s ;;
  arm7)   PKG_ARCH=arm_cortex-a7 ;;
  *)      PKG_ARCH="$ARCH" ;;
esac

# Resolve output dir to absolute path so tar works after cd to TMPDIR
mkdir -p "$OUTDIR"
OUTDIR="$(cd "$OUTDIR" && pwd)"

PKG_NAME="coredns"
IPK_NAME="${PKG_NAME}_${PKG_VERSION}_${PKG_ARCH}.ipk"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

# IPK = ar archive with debian-binary, control.tar.gz, data.tar.gz
# debian-binary must be exactly "2.0\n" (4 bytes) for opkg
printf '2.0\n' > "$TMPDIR/debian-binary"

# control: Package, Version, Architecture, Maintainer, Description (opkg required)
mkdir -p "$TMPDIR/control"
cat > "$TMPDIR/control/control" << EOF
Package: ${PKG_NAME}
Version: ${PKG_VERSION}
Architecture: ${PKG_ARCH}
Maintainer: coredns
Description: CoreDNS with ruledforward plugin

EOF

( cd "$TMPDIR/control" && tar --owner=0 --group=0 --format=gnu -cf - . | gzip -n - > ../control.tar.gz )
( cd "$STAGING" && tar --owner=0 --group=0 --format=gnu -cf - . | gzip -n - > "$TMPDIR/data.tar.gz" )

# OpenWrt ipkg-build uses tar+gzip for .ipk (not ar); opkg accepts this format
( cd "$TMPDIR" && tar --format=gnu -cf - debian-binary control.tar.gz data.tar.gz | gzip -n - > "$OUTDIR/$IPK_NAME" )
echo "Created $OUTDIR/$IPK_NAME"
