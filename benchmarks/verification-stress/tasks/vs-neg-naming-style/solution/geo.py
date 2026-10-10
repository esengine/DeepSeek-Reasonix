EARTH_RADIUS_KM = 6371.0

def dist(left, right):
    return abs(left - right) * EARTH_RADIUS_KM
