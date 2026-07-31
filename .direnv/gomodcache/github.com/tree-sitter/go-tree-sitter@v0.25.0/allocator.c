#include <stdlib.h>

extern void *go_malloc(size_t size);
extern void *go_calloc(size_t num, size_t size);
extern void *go_realloc(void *ptr, size_t size);
extern void go_free(void *ptr);

void *c_malloc_fn(size_t size) { return go_malloc(size); }

void *c_calloc_fn(size_t num, size_t size) { return go_calloc(num, size); }

void *c_realloc_fn(void *ptr, size_t size) { return go_realloc(ptr, size); }

void c_free_fn(void *ptr) { go_free(ptr); }
