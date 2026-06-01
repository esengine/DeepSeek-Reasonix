set -e
python3 - <<'PY'
import palindrome
assert palindrome.is_palindrome("Race car") is True
assert palindrome.is_palindrome("A man, a plan, a canal: Panama") is True
assert palindrome.is_palindrome("hello") is False
PY
