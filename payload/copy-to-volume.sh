#!/bin/sh
set -eu
mount_path=${MOUNT_PATH:-/__otel_sdk_auto_instrumentation__}
umask 022
mkdir -p "$mount_path/dist"
# Recopy on a retried init container so an interrupted copy cannot look complete.
cp -R /dist/. "$mount_path/dist/"
cmp /dist/versions.json "$mount_path/dist/versions.json"
test -s "$mount_path/dist/injector/libotelinject.so"
