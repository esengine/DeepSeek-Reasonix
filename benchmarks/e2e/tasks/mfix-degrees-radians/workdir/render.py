import math

from geometry import elevation_deg


def shadow_factor():
    """sin of the sun elevation; 0.5 at 30 degrees."""
    return math.sin(elevation_deg())
