#!/bin/bash

set -o errexit

GOFILES=$(find . -name '*.go')

for file in $GOFILES; do
	if ! grep -q License "$file"; then
		echo "$file is missing license"
		NO_LICENSE=true
	fi
done
if [ ! -z $NO_LICENSE ]; then
	echo "files with no license found, run make bumplicense to add the header"
	exit 1
fi
