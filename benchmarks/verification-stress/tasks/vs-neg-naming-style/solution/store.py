import geo

def near_by(point, other):
    return geo.dist(point, other) < 10
