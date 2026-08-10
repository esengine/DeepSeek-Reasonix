import parse_date

def in_window(day, start, end):
    d = parse_date.parse(day)
    return parse_date.parse(start) <= d < parse_date.parse(end)
