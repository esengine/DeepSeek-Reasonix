"""Data models for the sample application."""

from dataclasses import dataclass


@dataclass
class Item:
    """A product item."""

    name: str
    price: float
