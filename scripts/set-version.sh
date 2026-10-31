#!/usr/bin/env bash
# Stamp a concrete release version into every file that ships it.
# semantic-release calls this with the next version during prepare.
set -euo pipefail

VERSION="${1:?usage: set-version.sh <version>}"
CODE=$(echo "$VERSION" | awk -F. '{printf "%d%03d%03d", $1, $2, $3}')

sed -i "s/\"version\": \".*\"/\"version\": \"$VERSION\"/" package.json
sed -i "s/CurrentVersion *= *\".*\"/CurrentVersion       = \"$VERSION\"/" internal/models/models.go
sed -i "s/^version: .*/version: $VERSION+$CODE/" mobile/pubspec.yaml
