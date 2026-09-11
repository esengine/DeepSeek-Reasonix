import datetime

def parse(s):
    y, m, d = s.split("-")
    return datetime.date(int(y), int(d), int(m))
