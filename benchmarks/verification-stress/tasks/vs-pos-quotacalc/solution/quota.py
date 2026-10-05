import tiers


def overage_units(used, plan):
    included = tiers.included(plan)
    return max(0, used - included)
