import os
import requests
from typing import Any


def open_url(url: str, stream: bool = False, **kwargs) -> Any:
    """Open a URL and return the requests Response object.

    This is a small wrapper around `requests.get` to centralize HTTP access so
    tests can patch a single symbol. By default it sets a 10s timeout but
    callers can override via `timeout=` in kwargs.

    If the environment variable `FASTCP_DISABLE_NETWORK` is set to "1",
    this function raises RuntimeError to prevent accidental network access in
    CI or disabled environments.
    """
    if os.getenv("FASTCP_DISABLE_NETWORK") == "1":
        raise RuntimeError(
            "Network access disabled (FASTCP_DISABLE_NETWORK=1)"
        )

    timeout = kwargs.pop("timeout", 10)
    return requests.get(url, stream=stream, timeout=timeout, **kwargs)


def get_bytes(url: str, **kwargs) -> bytes:
    """Convenience helper that returns response bytes for small downloads."""
    with open_url(url, stream=False, **kwargs) as res:
        res.raise_for_status()
        return res.content
