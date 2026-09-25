#!/bin/sh
set -eu
cd "$(dirname "$0")"
javac="${JAVA_HOME:+$JAVA_HOME/bin/}javac"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
"$javac" --release 17 -Werror -Xlint:all -d "$tmp" $(find stubs src -name '*.java')
rm -rf classes
mkdir -p classes
cp -R "$tmp/shulker" classes/
