import convert

def band(c):
    f = convert.c_to_f(c)
    if f >= 90:
        return "hot"
    if f >= 60:
        return "warm"
    return "cold"
