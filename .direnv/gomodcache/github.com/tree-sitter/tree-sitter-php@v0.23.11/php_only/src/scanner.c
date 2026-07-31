#include "../../common/scanner.h"

void *tree_sitter_php_only_external_scanner_create() {
    return external_scanner_create();
}
unsigned tree_sitter_php_only_external_scanner_serialize(void *p, char *b) {
    return external_scanner_serialize(p, b);
}
void tree_sitter_php_only_external_scanner_deserialize(void *p, const char *b, unsigned n) {
    external_scanner_deserialize(p, b, n);
}
bool tree_sitter_php_only_external_scanner_scan(void *p, TSLexer *l, const bool *s) {
    return external_scanner_scan(p, l, s);
}
void tree_sitter_php_only_external_scanner_destroy(void *p) {
    external_scanner_destroy(p);
}
