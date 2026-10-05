from calc import total

assert total([1, 2, 3]) == 6, "positives must still sum"
assert total([1, -2, 3]) == 4, "negatives must be ignored"
print("check_a OK")
