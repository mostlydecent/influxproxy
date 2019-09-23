#!/bin/sh -l
set -x
set -e

_SHA=$(git rev-parse --short $INPUT_HASH)
_REF=$(echo $INPUT_REF | sed -r 's/refs\/(tags|heads)\///')

case "$_REF" in
  v[0-9]*) _VERSION=$(echo $_REF | cut -d'v' -f2) ;;
  *) _VERSION="0.0.0-${_SHA}" ;;
esac

echo ::set-output name=version::$_VERSION
echo ::set-output name=tag::v$_VERSION
