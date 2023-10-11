#!/bin/bash
set -e

if [ -z "$FRRK8S_VERSION" ]; then
    echo "must set the FRRK8S_VERSION environment variable"
    exit -1
fi


git commit -a -m "Automated update for release v$FRRK8S_VERSION"
git tag "v$FRRK8S_VERSION" -m 'See the release notes for details:\n\nhttps://raw.githubusercontent.com/metallb/frr-k8s/main/RELEASE_NOTES.md'
git checkout main
