#!/bin/bash
set -e

if [ -z "$FRRK8S_VERSION" ]; then
    echo "must set the FRRK8S_VERSION environment variable"
    exit -1
fi

gitstatus=$(git status --porcelain)
if [ -n "$gitstatus" ]; then
	echo "uncommitted changes"
	echo $gitstatus
	exit 1
fi


VERSION="$FRRK8S_VERSION"
VERSION="${VERSION#[vV]}"
VERSION_MAJOR="${VERSION%%\.*}"
VERSION_MINOR="${VERSION#*.}"
VERSION_MINOR="${VERSION_MINOR%.*}"
VERSION_PATCH="${VERSION##*.}"

git checkout main

if ! grep -q "## Release v$FRRK8S_VERSION" RELEASE_NOTES.md; then
  echo "Version $FRRK8S_VERSION missing from release notes"
  exit 1
fi

BRANCH_NAME="v$VERSION_MAJOR.$VERSION_MINOR"
if [ $minor = "0" ]; then # patch release
	git checkout -b $BRANCH_NAME
else
	git checkout $BRANCH_NAME
fi

git checkout main -- RELEASE_NOTES.md
