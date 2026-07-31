#include <stdlib.h>

void *c_malloc_fn(size_t size);

void *c_calloc_fn(size_t num, size_t size);

void *c_realloc_fn(void *ptr, size_t size);

void c_free_fn(void *ptr);
