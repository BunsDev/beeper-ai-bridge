#!/bin/sh
set -e
if [ "$#" -eq 0 ]; then
	echo "usage: $0 <go-package-or-file> [args...]" >&2
	exit 2
fi
go run "$@"
