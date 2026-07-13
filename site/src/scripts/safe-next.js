// A post-login `next` redirect target is honoured only if it resolves — via
// full URL parsing against the real page origin, so control characters and
// protocol-relative tricks get the same normalization a browser applies
// before navigating — to a same-origin path, or to an absolute https URL
// under reasonix.io. That lets a subdomain (e.g. crash.reasonix.io) return
// here after sign-in without opening a redirect to an arbitrary host.
//
// Validating a raw string prefix (e.g. checking it starts with "/") is not
// enough: URLSearchParams decodes percent-encoding, so a value like
// "/%09/evil.example" arrives as "/\t/evil.example", which passes a naive
// prefix check but the URL parser strips the tab during navigation, leaving
// "//evil.example" — a protocol-relative redirect off-site. Parsing with
// `new URL()` first and checking the *parsed* origin/host closes that gap
// because it uses the same normalization the browser itself applies.
//
// Both branches return the fully-serialized `u.href`, never a relative
// fragment. Returning `u.pathname` would reopen the hole: dot-segment inputs
// such as "/.//evil.example" or "/a/..//evil.example" resolve to a
// same-origin URL whose *pathname* is "//evil.example", and handing that
// bare pathname back to `location.href` re-parses it as a protocol-relative
// URL to evil.example. `u.href` keeps the origin attached, so assigning it
// can never leave reasonix.io.
export function safeNext(next, origin) {
  if (!next) return null;
  let u;
  try {
    u = new URL(next, origin);
  } catch {
    return null;
  }
  if (u.origin === origin) return u.href;
  if (u.protocol === "https:" && (u.host === "reasonix.io" || u.host.endsWith(".reasonix.io"))) return u.href;
  return null;
}
