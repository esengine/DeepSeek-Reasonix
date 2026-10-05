"""Entry point wiring."""

import app.models as models
from app import config, utils


def run() -> str:
    settings = config.get_settings()
    item = models.Item("widget", settings["timeout"])
    return utils.to_json(item)
