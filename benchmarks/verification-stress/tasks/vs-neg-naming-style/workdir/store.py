import geo

def NearBy(x, other):
    return geo.Dist(x, other) < 10
