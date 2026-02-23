#!/bin/sh
# Build Alpine .apk from staging. Intended to run inside an Alpine container with abuild.
# Staging is prepared by: make pack-apk (creates .build/pack/apk-*).
# In Alpine: copy staging into package dir, then run abuild or this script.
# This is a placeholder; full apk build typically uses APKBUILD and abuild.
set -e
echo "Alpine apk build: copy staging to a directory with APKBUILD and run: abuild -r"
echo "See https://wiki.alpinelinux.org/wiki/Creating_an_Alpine_package"
