ROWS = [("widget", 3, 4.5), ("gasket", 10, 0.25)]


def render():
    return "\n".join("%s %d %.2f" % row for row in ROWS)
