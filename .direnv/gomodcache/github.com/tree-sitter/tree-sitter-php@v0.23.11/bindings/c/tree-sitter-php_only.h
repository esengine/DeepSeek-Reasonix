#ifndef TREE_SITTER_PHP_ONLY_H_
#define TREE_SITTER_PHP_ONLY_H_

typedef struct TSLanguage TSLanguage;

#ifdef __cplusplus
extern "C" {
#endif

const TSLanguage *tree_sitter_php_only(void);

#ifdef __cplusplus
}
#endif

#endif // TREE_SITTER_PHP_ONLY_H_
