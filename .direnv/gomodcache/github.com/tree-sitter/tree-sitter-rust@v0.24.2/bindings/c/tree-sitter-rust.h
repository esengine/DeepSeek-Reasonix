#ifndef TREE_SITTER_RUST_H_
#define TREE_SITTER_RUST_H_

typedef struct TSLanguage TSLanguage;

#ifdef __cplusplus
extern "C" {
#endif

const TSLanguage *tree_sitter_rust(void);

#ifdef __cplusplus
}
#endif

#endif // TREE_SITTER_RUST_H_
