#!/bin/bash

set -o errexit

GOFILES=$(find . -name '*.go')

for file in $GOFILES; do
	if ! grep -q License "$file"; then
		echo "Bumping $file"
            	sed -i '1s/^/\/\/ SPDX-License-Identifier:Apache-2.0\n\n/' $file
	fi
done
