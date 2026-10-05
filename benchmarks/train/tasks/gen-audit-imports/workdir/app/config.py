"""Application configuration."""

import os

# Historical note: this module used to import dotenv; today it needs only os.
DEFAULT_TIMEOUT = 30


def get_settings():
    return {
        "debug": os.environ.get("APP_DEBUG", "0") == "1",
        "timeout": DEFAULT_TIMEOUT,
    }
