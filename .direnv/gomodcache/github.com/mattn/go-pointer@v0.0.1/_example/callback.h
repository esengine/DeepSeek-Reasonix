#include <unistd.h>

typedef void (*callback)(void*);

static void call_later(int delay, callback cb, void* data) {
  sleep(delay);
  cb(data);
}

