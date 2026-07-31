/**
 * Matches an ASCII tab (U+0009) or newline (U+000A / U+000D). The WHATWG URL
 * parser strips these before parsing; we only allocate a cleaned copy (and
 * re-parse) on the rare input that actually contains one.
 */
const CONTROL_CHARS = /[\t\n\r]/g;

// Set by `extractHostname` (a module-scope flag, read synchronously by
// `parseImpl` right after the call — same pattern as the reused RESULT object).
// `true` ONLY when extraction validated the returned host inline (a confirmed-
// valid, "simple" authority) so `parseImpl` can skip the separate
// `isValidHostname` pass. `false` in every other case (validation disabled, a
// complex authority — userinfo/port/brackets/trailing-dot/control — an invalid
// host, or a non-main return path); `parseImpl` then validates as usual. The
// fast path can only ever SKIP a redundant scan for hosts already known valid,
// never accept an invalid one.
export let extractedHostnameValidated = false;

/**
 * True if char `code` is a valid hostname character. This is the per-char half
 * of `is-valid.ts`'s `isValidAscii` (a-z, 0-9, > U+007F) PLUS three additions:
 * A-Z (the host is lowercased before validation, so uppercase ≡ a valid
 * lowercase letter) and '-' / '_' (valid inside a label). KEEP IN SYNC with
 * `is-valid.ts`: these rules are deliberately duplicated to validate during
 * extraction, so any change to the accepted character set there must be
 * mirrored here (and vice-versa).
 */
function isValidHostnameChar(code: number): boolean {
  return (
    (code >= 97 && code <= 122) || // a-z
    (code >= 48 && code <= 57) || // 0-9
    code > 127 || // non-ASCII (accepted, not punycode-checked)
    (code >= 65 && code <= 90) || // A-Z (becomes valid once lowercased)
    code === 45 || // '-'
    code === 95 // '_'
  );
}

/**
 * Classify scheme `url.slice(schemeStart, colonIndex)` as a WHATWG special
 * scheme without allocating a substring (case-insensitive via `| 32`).
 * Special schemes: ftp, file, http, https, ws, wss
 * (https://url.spec.whatwg.org/#special-scheme).
 *
 * @returns 0 = not special, 1 = special, 2 = file (its host sits only between
 *          "//" and the next slash).
 */
function getSpecialScheme(
  url: string,
  schemeStart: number,
  colonIndex: number,
): number {
  const length = colonIndex - schemeStart;
  const c0 = url.charCodeAt(schemeStart) | 32;
  if (length === 2) {
    return c0 === 119 && (url.charCodeAt(schemeStart + 1) | 32) === 115 ? 1 : 0; // ws
  } else if (length === 3) {
    const c1 = url.charCodeAt(schemeStart + 1) | 32;
    const c2 = url.charCodeAt(schemeStart + 2) | 32;
    if (c0 === 119 && c1 === 115 && c2 === 115) return 1; // wss
    if (c0 === 102 && c1 === 116 && c2 === 112) return 1; // ftp
    return 0;
  } else if (length === 4) {
    const c1 = url.charCodeAt(schemeStart + 1) | 32;
    const c2 = url.charCodeAt(schemeStart + 2) | 32;
    const c3 = url.charCodeAt(schemeStart + 3) | 32;
    if (c0 === 104 && c1 === 116 && c2 === 116 && c3 === 112) return 1; // http
    if (c0 === 102 && c1 === 105 && c2 === 108 && c3 === 101) return 2; // file
    return 0;
  } else if (length === 5) {
    return c0 === 104 &&
      (url.charCodeAt(schemeStart + 1) | 32) === 116 &&
      (url.charCodeAt(schemeStart + 2) | 32) === 116 &&
      (url.charCodeAt(schemeStart + 3) | 32) === 112 &&
      (url.charCodeAt(schemeStart + 4) | 32) === 115
      ? 1
      : 0; // https
  }
  return 0;
}

/**
 * Extract a hostname from `url`, matching a WHATWG URL parser's host-boundary
 * behaviour (https://url.spec.whatwg.org/#concept-basic-url-parser) for tldts'
 * scope. It deliberately does NOT normalise the host (no IDNA/punycode or IPv4
 * canonicalisation; IPv6 brackets are stripped, not compressed), strips trailing
 * dots, and stays lenient where a strict parser rejects (bare host:port,
 * out-of-range port, user@host) — all documented deviations.
 *
 * @param urlIsValidHostname - when true, `url` is already a valid hostname and is
 *   returned by the same reference (factory.ts skips re-validation on that
 *   identity), keeping the common path allocation-free.
 * @param validate - when true, validate the host inline during the authority
 *   scan and publish the verdict via `extractedHostnameValidated` so `parseImpl`
 *   can skip the redundant `isValidHostname` pass for simple authorities.
 */
export default function extractHostname(
  url: string,
  urlIsValidHostname: boolean,
  validate = false,
): string | null {
  let start = 0;
  let end: number = url.length;
  let hasUpper = false;
  let isSpecial = false;
  extractedHostnameValidated = false;

  if (!urlIsValidHostname) {
    // Data URLs never carry a host (and may be huge — short-circuit them).
    if (url.startsWith('data:')) {
      return null;
    }

    // WHATWG step 1: trim leading/trailing C0 control or space (<= U+0020).
    // Tab/newline elsewhere are handled lazily below.
    while (start < url.length && url.charCodeAt(start) <= 32) {
      start += 1;
    }
    while (end > start + 1 && url.charCodeAt(end - 1) <= 32) {
      end -= 1;
    }

    if (
      url.charCodeAt(start) === 47 /* '/' */ &&
      url.charCodeAt(start + 1) === 47 /* '/' */
    ) {
      // Scheme-relative reference ("//host/path").
      start += 2;
    } else {
      const indexOfProtocol = url.indexOf(':/', start);
      if (indexOfProtocol !== -1) {
        // "scheme://…". Classify the scheme, then position `start` at the host.
        const special = getSpecialScheme(url, start, indexOfProtocol);
        if (special === 1) {
          // Special scheme: skip the run of '/' and '\' after it
          // (special-authority-(ignore-)slashes states; '\' acts as '/').
          isSpecial = true;
          start = indexOfProtocol + 2;
          while (
            url.charCodeAt(start) === 47 /* '/' */ ||
            url.charCodeAt(start) === 92 /* '\' */
          ) {
            start += 1;
          }
        } else if (special === 2) {
          // file: the host is only what sits between "//" and the next slash, so
          // "file://h/x" => "h" but "file:///x" / "file:/x" => no host.
          isSpecial = true;
          start = indexOfProtocol + 1;
          let slashes = 0;
          while (
            (url.charCodeAt(start) === 47 || url.charCodeAt(start) === 92) &&
            slashes < 2
          ) {
            start += 1;
            slashes += 1;
          }
          if (slashes < 2) {
            return null;
          }
        } else {
          // Unknown scheme: validate the WHATWG scheme grammar [A-Za-z0-9+.-];
          // a control char means it was split by a tab/newline (strip + re-parse).
          for (let i = start; i < indexOfProtocol; i += 1) {
            const code = url.charCodeAt(i) | 32;
            if (
              !(
                (
                  (code >= 97 && code <= 122) || // [a, z]
                  (code >= 48 && code <= 57) || // [0, 9]
                  code === 46 || // '.'
                  code === 45 || // '-'
                  code === 43
                ) // '+'
              )
            ) {
              const raw = url.charCodeAt(i);
              if (raw === 9 || raw === 10 || raw === 13) {
                return extractHostname(
                  url.replace(CONTROL_CHARS, ''),
                  urlIsValidHostname,
                  validate,
                );
              }
              return null;
            }
          }
          // A non-special scheme has an authority only after "//" (else it is an
          // opaque path with no host). `indexOf(':/')` already gave the first '/'.
          if (url.charCodeAt(indexOfProtocol + 2) === 47 /* '/' */) {
            start = indexOfProtocol + 3;
          } else {
            return null;
          }
        }
      } else if (url.charCodeAt(start) !== 91 /* '[' */) {
        // Cold path: no scheme "://", and not a bare IPv6 literal (whose first
        // ':' would otherwise look like a scheme separator; "[…]" falls through
        // to the ipv6 handling below). May be a bare host, a host:port, a
        // user@host, a slash-less special scheme ("https:host"), or an opaque
        // URI ("mailto:", "tel:", "urn:…").
        let indexOfColon = -1;
        for (let i = start; i < end; i += 1) {
          const code = url.charCodeAt(i);
          if (code === 9 || code === 10 || code === 13) {
            return extractHostname(
              url.replace(CONTROL_CHARS, ''),
              urlIsValidHostname,
              validate,
            );
          }
          if (code === 58 /* ':' */) {
            indexOfColon = i;
            break;
          }
          if (code === 47 || code === 92 || code === 63 || code === 35) {
            break;
          }
        }

        if (indexOfColon !== -1) {
          // An '@' before the next delimiter => the ':' is userinfo, not a
          // scheme ("user:pass@host", "mailto:a@b"): keep the whole authority.
          let hasIdentifier = false;
          for (let i = indexOfColon + 1; i < end; i += 1) {
            const code = url.charCodeAt(i);
            if (code === 47 || code === 92 || code === 63 || code === 35) {
              break;
            }
            if (code === 64 /* '@' */) {
              hasIdentifier = true;
              break;
            }
          }

          if (!hasIdentifier) {
            // All-digits after ':' => a bare "host:port" (tldts accepts
            // hostnames too); keep `start` and let the port handling trim it.
            let allDigits = true;
            let i = indexOfColon + 1;
            for (; i < end; i += 1) {
              const code = url.charCodeAt(i);
              if (code === 47 || code === 92 || code === 63 || code === 35) {
                break;
              }
              if (code < 48 /* '0' */ || code > 57 /* '9' */) {
                allDigits = false;
                break;
              }
            }
            if (i === indexOfColon + 1) {
              allDigits = false; // nothing after ':' => not a port
            }

            if (!allDigits) {
              const special = getSpecialScheme(url, start, indexOfColon);
              if (special === 0) {
                // No "://" anywhere on the cold path and not a special scheme.
                // A second ':' before the host's end marks a bare, unbracketed
                // IPv6 literal ("2a01:e35::1"): fall through and let the host
                // loop + isIp classify it. Without one this is an opaque path
                // with no host ("mailto:x", "foo:bar").
                let isBareIpv6 = false;
                for (let j = indexOfColon + 1; j < end; j += 1) {
                  const code = url.charCodeAt(j);
                  if (
                    code === 47 ||
                    code === 92 ||
                    code === 63 ||
                    code === 35
                  ) {
                    break;
                  }
                  if (code === 58 /* ':' */) {
                    isBareIpv6 = true;
                    break;
                  }
                }
                if (!isBareIpv6) {
                  return null;
                }
              } else {
                isSpecial = true;
                start = indexOfColon + 1;
                if (special === 2) {
                  // file (e.g. "file:\\host"): host only between "//" and next slash.
                  let slashes = 0;
                  while (
                    (url.charCodeAt(start) === 47 ||
                      url.charCodeAt(start) === 92) &&
                    slashes < 2
                  ) {
                    start += 1;
                    slashes += 1;
                  }
                  if (slashes < 2) {
                    return null;
                  }
                } else {
                  while (
                    url.charCodeAt(start) === 47 ||
                    url.charCodeAt(start) === 92
                  ) {
                    start += 1;
                  }
                }
              }
            }
          }
        }
      }
    }

    // Find the host's end: first '/', '?' or '#' (and '\' for special URLs,
    // which WHATWG treats like '/'). Track the last '@', ']' and ':' for
    // userinfo, ipv6 and port, plus the first ':' of the host (reset at each
    // '@') to tell a bare IPv6 (>= 2 colons) from a host:port (exactly one);
    // flag uppercase and a stray tab/newline. The loop is split on `code < 64`
    // so common host characters take fewer comparisons.
    //
    // When `validate`, also accumulate `is-valid.ts`'s checks over the scanned
    // run so a simple authority's host can be validated in this single pass.
    // `vValid` only stays meaningful for a "simple" authority (no userinfo, port,
    // brackets, control or trailing dot); those cases clear it / are rejected by
    // the guard below, falling back to `isValidHostname`.
    let indexOfIdentifier = -1;
    let indexOfClosingBracket = -1;
    let indexOfPort = -1;
    let indexOfFirstColon = -1;
    let hasControl = false;
    let vValid = validate; // seeded true when validating; cleared on the first invalid char
    let vLastDot = start - 1; // mirrors is-valid.ts `lastDotIndex = -1` at host start
    let vLastCode = -1;
    if (validate && start < end) {
      // First-char rule: must be a valid host char, '.', or '_' (NOT '-').
      const c0 = url.charCodeAt(start);
      if (
        !(
          /*@__INLINE__*/ (
            isValidHostnameChar(c0) ||
            c0 === 46 /* '.' */ ||
            c0 === 95 /* '_' */
          )
        ) ||
        c0 === 45 /* '-' (isValidHostnameChar allows it mid-label, not first) */
      ) {
        vValid = false;
      }
    }
    for (let i = start; i < end; i += 1) {
      const code: number = url.charCodeAt(i);
      if (code < 64) {
        if (code === 47 || code === 35 || code === 63) {
          end = i;
          break;
        } else if (code === 58 /* ':' */) {
          if (indexOfFirstColon === -1) {
            indexOfFirstColon = i;
          }
          indexOfPort = i;
        } else if (code === 9 || code === 10 || code === 13) {
          hasControl = true;
        } else if (validate) {
          if (code === 46 /* '.' */) {
            if (i - vLastDot > 64 || vLastCode === 46 || vLastCode === 45) {
              vValid = false;
            }
            vLastDot = i;
          } else if (code < 48 || code > 57) {
            // < 64 and not a delimiter/dot/digit => only '-' (45) is a valid
            // host char here; everything else (space, %, !, etc.) is invalid.
            // A '-' must also not START a label (the byte right after a '.') —
            // mirrors is-valid.ts; the first label is covered by the first-char
            // rule above. (RFC 1034 §3.5 / RFC 1035 §2.3.1 LDH.)
            if (code !== 45 || vLastCode === 46 /* label-leading '-' */) {
              vValid = false;
            }
          }
        }
      } else if (isSpecial && code === 92 /* '\' */) {
        end = i;
        break;
      } else if (code === 64 /* '@' */) {
        indexOfIdentifier = i;
        indexOfFirstColon = -1; // colons before '@' are userinfo, not the host
      } else if (code === 93 /* ']' */) {
        indexOfClosingBracket = i;
      } else if (code >= 65 && code <= 90) {
        hasUpper = true;
      } else if (validate && !(/*@__INLINE__*/ isValidHostnameChar(code))) {
        // >= 64, not '@'/']'/upper: valid only if a-z, '_', or non-ASCII.
        vValid = false;
      }
      if (validate) {
        vLastCode = code;
      }
    }

    // A tab/newline inside the authority: strip everything and re-parse (rare).
    if (hasControl) {
      return extractHostname(
        url.replace(CONTROL_CHARS, ''),
        urlIsValidHostname,
        validate,
      );
    }

    // Skip userinfo. '>= start' so an empty userinfo ("http://@host") works too.
    if (
      indexOfIdentifier !== -1 &&
      indexOfIdentifier >= start &&
      indexOfIdentifier < end
    ) {
      start = indexOfIdentifier + 1;
    }

    if (url.charCodeAt(start) === 91 /* '[' */) {
      // ipv6 address: return what is between the brackets, or null if unclosed.
      if (indexOfClosingBracket !== -1) {
        return url.slice(start + 1, indexOfClosingBracket).toLowerCase();
      }
      return null;
    } else if (
      indexOfPort !== -1 &&
      indexOfPort > start &&
      indexOfPort < end &&
      // A host:port has exactly one ':' in the host (so its first ':' is its
      // last); a bare, unbracketed IPv6 literal ("2a01:e35::1") has >= 2, so
      // its first ':' precedes the last. Only the former has a ':port' to trim.
      indexOfFirstColon === indexOfPort
    ) {
      end = indexOfPort; // trim ':port'
    }

    // Empty authority ("http://", "file:///path", "//"); only reachable here via
    // extraction — a bare valid hostname never lands here.
    if (start >= end) {
      return null;
    }

    // Publish the inline-validation verdict — but only for a "simple" authority,
    // where the scanned run equals the final host: no userinfo skip, no port
    // trim, no brackets, no trailing dot (trimmed below), and length within RFC
    // limits. Anything else leaves it `false` so `parseImpl` re-validates.
    //
    // Every clause below is load-bearing for CORRECTNESS, not just speed: the
    // loop accumulates `vValid` over the whole scanned run (it does not stop at
    // ':' or '@', so any port/userinfo bytes are included), so the verdict is
    // only sound when that run equals the final host. Do not drop a clause as
    // "redundant" — e.g. without `indexOfPort === -1`, `host:8080` would be
    // wrongly accepted.
    if (
      validate &&
      vValid &&
      indexOfIdentifier === -1 &&
      indexOfPort === -1 &&
      indexOfClosingBracket === -1 &&
      url.charCodeAt(end - 1) !== 46 /* no trailing dot */ &&
      end - start <= 255 && // total length
      end - vLastDot - 1 <= 63 && // last label length
      vLastCode !== 45 /* last char not '-' */
    ) {
      extractedHostnameValidated = true;
    }
  }

  // Trim trailing dots
  while (end > start + 1 && url.charCodeAt(end - 1) === 46 /* '.' */) {
    end -= 1;
  }

  const hostname: string =
    start !== 0 || end !== url.length ? url.slice(start, end) : url;

  if (hasUpper) {
    return hostname.toLowerCase();
  }

  return hostname;
}
