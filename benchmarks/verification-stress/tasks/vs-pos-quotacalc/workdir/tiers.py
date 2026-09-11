PLANS = {"basic": 100, "pro": 1000}

def included(plan):
    return PLANS[plan]

def tier_for(units):
    if units > 100:
        return "pro"
    return "basic"
