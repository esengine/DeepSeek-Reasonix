package tree_sitter

/*
#cgo CFLAGS: -Iinclude -Isrc -std=c11 -D_POSIX_C_SOURCE=200112L -D_DEFAULT_SOURCE
#include <tree_sitter/api.h>
#include "lib.c" // <- This is needed to build the C library from the C source code, but cannot be included in files that have other declarations.
*/
import "C"
