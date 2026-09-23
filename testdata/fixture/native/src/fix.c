#include "fix/fix.h"
#include <stdlib.h>

static int helper(int x)
{
  return x + 1;
}

void *fix_easy_init(void)
{
  const char *home = getenv("FIX_HOME");
  return (void *)home;
}

int fix_easy_perform(void *handle, fixoption option)
{
  return helper((int)option);
}
