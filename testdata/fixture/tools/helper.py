"""Fixture helper module."""

MAX_RETRIES = 3


class Runner:
    def run(self, cmd):
        return cmd

    async def run_async(self, cmd):
        return cmd


def summarize(items):
    return len(items)
