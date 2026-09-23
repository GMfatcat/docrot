#ifndef FIX_H
#define FIX_H

#define FIX_MAX_ITEMS 8

typedef enum {
  FIXOPT_URL,
  FIXOPT_PORT
} fixoption;

/* fix_easy_init returns a handle. */
void *fix_easy_init(void);
int fix_easy_perform(void *handle, fixoption option);

#endif
