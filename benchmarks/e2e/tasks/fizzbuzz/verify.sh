set -e
python3 - <<'PY'
import fizzbuzz
assert fizzbuzz.fizzbuzz(3) == "Fizz", fizzbuzz.fizzbuzz(3)
assert fizzbuzz.fizzbuzz(5) == "Buzz", fizzbuzz.fizzbuzz(5)
assert fizzbuzz.fizzbuzz(15) == "FizzBuzz", fizzbuzz.fizzbuzz(15)
assert fizzbuzz.fizzbuzz(7) == "7", fizzbuzz.fizzbuzz(7)
PY
