#!/usr/bin/env bash

SRC_DIR="tree-sitter/lib"

if [ ! -d "$SRC_DIR/src" ] || [ ! -d "$SRC_DIR/include" ]; then
	echo "Error: source directories do not exist."
	exit 1
fi

cp -r "$SRC_DIR/src/" "."
cp -r "$SRC_DIR/include/" "."
