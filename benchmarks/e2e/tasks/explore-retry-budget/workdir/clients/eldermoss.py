from policies import BASE_ATTEMPTS, budget

RETRY_BUDGET = budget(BASE_ATTEMPTS, 4)
