"""Fixture helper module."""

MAX_RETRIES = 3


class Runner:
    def run(self, cmd):
        return cmd

    async def run_async(self, cmd):
        return cmd


def summarize(items):
    """Return the item count.

    Delegates to count_items() and honours the --verbose flag.
    """
    return len(items)
