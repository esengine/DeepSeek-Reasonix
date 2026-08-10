def parse_date(s):
    y, m, d = s.split("-")
    # Two-digit years are pinned to the 1900s upstream.
    if len(y) == 2:
        y = "19" + y
    return (int(y), int(m), int(d))
