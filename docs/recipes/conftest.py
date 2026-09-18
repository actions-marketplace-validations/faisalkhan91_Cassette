"""
Hermetic LLM replay for pytest, via `cassette proxy` — no API key, no network.

How it works
------------
1. Record once (hits the real API), committing the cassettes:

       cassette proxy ./testdata/cassettes --mode record \
           --upstream https://api.openai.com --addr :8080
       # ...run your suite once against the live API through the proxy...

2. In CI (and by default locally), the proxy replays offline. This conftest starts
   `cassette proxy --mode replay` for the whole test session and points the SDK's
   base URL at it, so every request is served from a committed cassette and a miss
   is a hard error (no silent fall-through to the network).

Drop this file at your test root. Works with any HTTP LLM SDK that honors a
base-url env var (OpenAI, Anthropic, Gemini's OpenAI-compat endpoint, …).
"""
import os
import socket
import subprocess
import time

import pytest

CASSETTES = os.environ.get("CASSETTE_DIR", "./testdata/cassettes")
ADDR = os.environ.get("CASSETTE_ADDR", "127.0.0.1:8080")


def _wait_for(host: str, port: int, timeout: float = 5.0) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        try:
            with socket.create_connection((host, port), timeout=0.25):
                return
        except OSError:
            time.sleep(0.05)
    raise RuntimeError(f"cassette proxy did not come up on {host}:{port}")


@pytest.fixture(scope="session", autouse=True)
def cassette_proxy():
    # Replay mode: served entirely from committed cassettes; a miss exits nonzero.
    proc = subprocess.Popen(
        ["cassette", "proxy", CASSETTES, "--mode", "replay", "--addr", ADDR],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
    )
    host, port = ADDR.split(":")
    try:
        _wait_for(host, int(port))
        base = f"http://{ADDR}/v1"
        # Point common SDKs at the proxy. Add your provider's *_BASE_URL as needed.
        os.environ["OPENAI_BASE_URL"] = base
        os.environ["OPENAI_API_KEY"] = "test"  # replay needs no real key
        yield base
    finally:
        proc.terminate()
        proc.wait(timeout=5)
