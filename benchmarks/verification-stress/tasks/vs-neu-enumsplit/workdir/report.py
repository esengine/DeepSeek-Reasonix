import states

def summarize(orders):
    return {s: sum(1 for o in orders if o == s) for s in states.STATES}
