# DOMPurify

[![npm](https://img.shields.io/npm/v/dompurify.svg)](https://www.npmjs.com/package/dompurify) [![License](https://img.shields.io/badge/license-MPL--2.0%20OR%20Apache--2.0-blue.svg)](https://github.com/cure53/DOMPurify/blob/main/LICENSE) [![Downloads](https://img.shields.io/npm/dm/dompurify.svg)](https://www.npmjs.com/package/dompurify) [![dependents](https://badgen.net/github/dependents-repo/cure53/dompurify?color=green&label=dependents)](https://github.com/cure53/DOMPurify/network/dependents) ![npm package minimized gzipped size (select exports)](https://img.shields.io/bundlejs/size/dompurify?color=%233C1&label=gzip) [![Cloudback](https://app.cloudback.it/badge/cure53/DOMPurify)](https://cloudback.it)

[![OpenSSF Best Practices](https://www.bestpractices.dev/projects/12162/badge)](https://www.bestpractices.dev/projects/12162) [![Build & Test](https://github.com/cure53/DOMPurify/actions/workflows/build-and-test.yml/badge.svg?branch=main)](https://github.com/cure53/DOMPurify/actions/workflows/build-and-test.yml) [![OpenSSF Scorecard](https://api.scorecard.dev/projects/github.com/cure53/DOMPurify/badge)](https://scorecard.dev/viewer/?uri=github.com/cure53/DOMPurify) [![Socket Badge](https://badge.socket.dev/npm/package/dompurify/latest)](https://badge.socket.dev/npm/package/dompurify/latest) [![snyk.io package health](https://img.shields.io/badge/snyk.io%20package%20health-97%2F100-brightgreen)](https://security.snyk.io/package/npm/dompurify)

DOMPurify is a DOM-only, super-fast, uber-tolerant XSS sanitizer for HTML, MathML and SVG.

It's also very simple to use and get started with. DOMPurify was [started in February 2014](https://github.com/cure53/DOMPurify/commit/a630922616927373485e0e787ab19e73e3691b2b) and, meanwhile, has reached version **v3.4.12**.

DOMPurify runs as JavaScript and works in all modern browsers (Safari (10+), Opera (15+), Edge, Firefox and Chrome - as well as almost anything else using Blink, Gecko or WebKit). It doesn't break on MSIE or other legacy browsers. It simply does nothing.

**Note that [DOMPurify v2.5.9](https://github.com/cure53/DOMPurify/releases/tag/2.5.9) is the latest version supporting MSIE. For important security updates compatible with MSIE, please use the [2.x branch](https://github.com/cure53/DOMPurify/tree/2.x).**

Our automated tests cover 9 browser/OS combinations on the current engines (Chromium, Firefox, and WebKit across Ubuntu, macOS, and Windows) on every push, and a separate matrix re-runs the suite on older engine snapshots (back to roughly Chromium 110, Firefox 108 and WebKit 16.4, around three years old) so regressions on outdated browsers get caught too. We also run Node.js v20, v22, v24, v25 and v26 with DOMPurify on [jsdom](https://github.com/jsdom/jsdom). Older Node versions are known to work as well, but hey... no guarantees.

DOMPurify is written by security people who have vast background in web attacks and XSS. Fear not. For more details please also read about our [Security Goals & Threat Model](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model). Please, read it. Like, really. And if you enjoy the gory details, the [Attack Classes & Bypass History](https://github.com/cure53/DOMPurify/wiki/Attack-Classes-&-Bypass-History) page catalogs the parser-mutation, namespace, clobbering, and template tricks DOMPurify defends against.

The DOMPurify project inspired the creation of the [HTML Sanitizer API](https://wicg.github.io/sanitizer-api/#sanitizer), which is already shipping in [many browsers](https://developer.mozilla.org/en-US/docs/Web/API/HTML_Sanitizer_API#browser_compatibility). The same capability is now being standardized directly in the [WHATWG HTML specification](https://html.spec.whatwg.org/#html-sanitization).

## Table of Contents

- [What does it do?](#what-does-it-do)
- [How do I use it?](#how-do-i-use-it)
- [Is there a demo?](#is-there-a-demo)
- [What if I find a _security_ bug?](#what-if-i-find-a-security-bug)
- [Some purification samples please?](#some-purification-samples-please)
- [What is supported?](#what-is-supported)
- [What about legacy browsers like Internet Explorer?](#what-about-legacy-browsers-like-internet-explorer)
- [What about DOMPurify and Trusted Types?](#what-about-dompurify-and-trusted-types)
- [Can I configure DOMPurify?](#can-i-configure-dompurify)
- [Persistent Configuration](#persistent-configuration)
- [Hooks](#hooks)
- [Removed Configuration](#removed-configuration)
- [Continuous Integration](#continuous-integration)
- [Security Mailing List](#security-mailing-list)
- [Who contributed?](#who-contributed)

## What does it do?

DOMPurify sanitizes HTML and prevents XSS attacks. You can feed DOMPurify with e.g. a string full of dirty HTML and it will return a string (unless configured otherwise) with clean HTML. DOMPurify will strip out everything that contains dangerous HTML and thereby prevent XSS attacks and other nastiness. It's also damn bloody fast. We use the technologies the browser provides and turn them into an XSS filter. The faster your browser, the faster DOMPurify will be.

## How do I use it?

It's easy. Just include DOMPurify on your website.

### Using the unminified version (source-map available)

```html
<script type="text/javascript" src="dist/purify.js"></script>
```

### Using the minified and tested production version (source-map available)

```html
<script type="text/javascript" src="dist/purify.min.js"></script>
```

Afterwards you can sanitize strings by executing the following code:

```js
const clean = DOMPurify.sanitize(dirty);
```

Or maybe this, if you love working with Angular or alike:

```js
import DOMPurify from 'dompurify';

const clean = DOMPurify.sanitize('<b>hello there</b>');
```

The resulting HTML can be written into a DOM element using `innerHTML` or the DOM using `document.write()`. That is fully up to you.
Note that by default, we permit HTML, SVG **and** MathML. If you only need HTML, which might be a very common use-case, you can easily set that up as well:

```js
const clean = DOMPurify.sanitize(dirty, { USE_PROFILES: { html: true } });
```

### Is there any foot-gun potential?

Well, please note, if you _first_ sanitize HTML and then modify it _afterwards_, you might easily **void the effects of sanitization**. If you feed the sanitized markup to another library _after_ sanitization, please be certain that the library doesn't mess around with the HTML on its own. See the [Security Goals & Threat Model](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model) for safe-usage recipes and the tags/attributes worth thinking twice about, and [Attack Classes & Bypass History](https://github.com/cure53/DOMPurify/wiki/Attack-Classes-&-Bypass-History) for why post-processing and changing the markup context defeat sanitization.

### Okay, makes sense, let's move on

After sanitizing your markup, you can also have a look at the property `DOMPurify.removed` and find out, what elements and attributes were thrown out. Please **do not use** this property for making any security critical decisions. This is just a little helper for curious minds.

### Running DOMPurify on the server

DOMPurify technically also works server-side with Node.js. Our support strives to follow the [Node.js release cycle](https://nodejs.org/en/about/previous-releases).

Running DOMPurify on the server requires a DOM to be present, which is probably no surprise. Usually, [jsdom](https://github.com/jsdom/jsdom) is the tool of choice and we **strongly recommend** to use the latest version of _jsdom_.

Why? Because older versions of _jsdom_ are known to be buggy in ways that result in XSS _even if_ DOMPurify does everything 100% correctly. There are **known attack vectors** in, e.g. _jsdom v19.0.0_ that are fixed in _jsdom v20.0.0_ - and we really recommend to keep _jsdom_ up to date because of that.

Please also be aware that tools like [happy-dom](https://github.com/capricorn86/happy-dom) exist but **are not considered safe** at this point. Combining DOMPurify with _happy-dom_ is currently not recommended and will likely lead to XSS. For background on why the server-side DOM you choose is part of your trusted computing base, see [Attack Classes & Bypass History](https://github.com/cure53/DOMPurify/wiki/Attack-Classes-&-Bypass-History).

Other than that, you are fine to use DOMPurify on the server. Probably. This really depends on _jsdom_ or whatever DOM you utilize server-side. If you can live with that, this is how you get it to work:

```bash
npm install dompurify
npm install jsdom
```

For _jsdom_ (please use an up-to-date version), this should do the trick:

```js
const createDOMPurify = require('dompurify');
const { JSDOM } = require('jsdom');

const window = new JSDOM('').window;
const DOMPurify = createDOMPurify(window);
const clean = DOMPurify.sanitize('<b>hello there</b>');
```

Or even this, if you prefer working with imports:

```js
import { JSDOM } from 'jsdom';
import DOMPurify from 'dompurify';

const window = new JSDOM('').window;
const purify = DOMPurify(window);
const clean = purify.sanitize('<b>hello there</b>');
```

If you have problems making it work in your specific setup, consider looking at the amazing [isomorphic-dompurify](https://github.com/kkomelin/isomorphic-dompurify) project which solves lots of problems people might run into.

```bash
npm install isomorphic-dompurify
```

```js
import DOMPurify from 'isomorphic-dompurify';

const clean = DOMPurify.sanitize('<s>hello</s>');
```

## Is there a demo?

Of course there is a demo! [Play with DOMPurify](https://cure53.de/purify)

## What if I find a security bug?

First of all, please immediately contact us via [email](mailto:mario@cure53.de) so we can work on a fix. [PGP key](https://keyserver.ubuntu.com/pks/lookup?op=vindex&search=0xC26C858090F70ADA)

Also, you probably qualify for a bug bounty! The fine folks over at [Fastmail](https://www.fastmail.com/) use DOMPurify for their services and added our library to their bug bounty scope. So, if you find a way to bypass or weaken DOMPurify, please also have a look at their website and the [bug bounty info](https://www.fastmail.com/about/bugbounty/).

## Some purification samples please?

How does purified markup look like? Well, [the demo](https://cure53.de/purify) shows it for a big bunch of nasty elements. But let's also show some smaller examples!

```js
DOMPurify.sanitize('<img src=x onerror=alert(1)//>'); // becomes <img src="x">
DOMPurify.sanitize('<svg><g/onload=alert(2)//<p>'); // becomes <svg><g></g></svg>
DOMPurify.sanitize('<p>abc<iframe//src=jAva&Tab;script:alert(3)>def</p>'); // becomes <p>abc</p>
DOMPurify.sanitize('<math><mi//xlink:href="data:x,<script>alert(4)</script>">'); // becomes <math><mi></mi></math>
DOMPurify.sanitize('<TABLE><tr><td>HELLO</tr></TABL>'); // becomes <table><tbody><tr><td>HELLO</td></tr></tbody></table>
DOMPurify.sanitize('<UL><li><A HREF=//google.com>click</UL>'); // becomes <ul><li><a href="//google.com">click</a></li></ul>
```

These are just a taste. For the full taxonomy of attack classes these samples come from - mutation XSS, namespace confusion, DOM clobbering, rawtext breakouts, and more - see [Attack Classes & Bypass History](https://github.com/cure53/DOMPurify/wiki/Attack-Classes-&-Bypass-History).

## What is supported?

DOMPurify currently supports HTML5, SVG and MathML. DOMPurify per default allows CSS, HTML custom data attributes. DOMPurify also supports the Shadow DOM - and sanitizes DOM templates recursively. DOMPurify also allows you to sanitize HTML for being used with the jQuery `$()` and `elm.html()` API without any known problems. For the exact set of elements and attributes permitted by default, see the [Default TAGs & ATTRIBUTEs allow-list & blocklist](https://github.com/cure53/DOMPurify/wiki/Default-TAGs-ATTRIBUTEs-allow-list-&-blocklist) wiki page.

## What about legacy browsers like Internet Explorer?

DOMPurify does nothing at all. It simply returns exactly the string that you fed it. DOMPurify exposes a property called `isSupported`, which tells you whether it will be able to do its job, so you can come up with your own backup plan.

## What about DOMPurify and Trusted Types?

In version 1.0.9, support for the [Trusted Types API](https://github.com/w3c/webappsec-trusted-types) ([MDN](https://developer.mozilla.org/en-US/docs/Web/API/Trusted_Types_API)) was added to DOMPurify.
In version 2.0.0, a config flag was added to control DOMPurify's behavior regarding this.

When `DOMPurify.sanitize` is used in an environment where the Trusted Types API is available and `RETURN_TRUSTED_TYPE` is set to `true`, it tries to return a `TrustedHTML` value instead of a string (the behavior for `RETURN_DOM` and `RETURN_DOM_FRAGMENT` config options does not change).

Note that in order to create a policy in `trustedTypes` using DOMPurify, `RETURN_TRUSTED_TYPE: false` is required, as `createHTML` expects a normal string, not `TrustedHTML`. The example below shows this.

```js
window.trustedTypes.createPolicy('default', {
  createHTML: (to_escape) =>
    DOMPurify.sanitize(to_escape, { RETURN_TRUSTED_TYPE: false }),
});
```

When no `TRUSTED_TYPES_POLICY` is supplied, DOMPurify attempts to create its own internal Trusted Types policy named `dompurify`. If your page already defines its own policy together with a strict CSP (for example `trusted-types my-organization`) that does not allow a policy named `dompurify`, this attempt is blocked by the browser and logs a `TrustedTypes policy dompurify could not be created.` warning along with a CSP violation.

To stop DOMPurify from creating its internal fallback policy, pass `TRUSTED_TYPES_POLICY: null`. This is the right choice when you call `DOMPurify.sanitize` from inside your own policy's `createHTML`, and it means you do not have to add `dompurify` to your CSP's `trusted-types` allowlist.

```js
window.trustedTypes.createPolicy('my-organization', {
  createHTML: (input) =>
    DOMPurify.sanitize(input, { TRUSTED_TYPES_POLICY: null }),
});
```

Do **not** pass your own wrapping policy back to DOMPurify as its `TRUSTED_TYPES_POLICY` (for example via `DOMPurify.setConfig({ TRUSTED_TYPES_POLICY: myPolicy })`) when that policy's `createHTML` already calls `DOMPurify.sanitize`. That is circular by definition - sanitizing would call the policy, which sanitizes by calling DOMPurify again - and DOMPurify will throw a descriptive `TypeError` to prevent the infinite recursion. Your own policy should call DOMPurify; DOMPurify should not be configured to call your policy.

If you want this `default`-policy pattern applied across an entire page automatically - so that every HTML sink is sanitized, including legacy code, third-party widgets, and the thousands of `innerHTML` assignments you cannot easily find or rewrite - have a look at [DOMFortify](https://github.com/cure53/DOMFortify). It installs exactly such a Trusted Types `default` policy backed by DOMPurify and refuses script sinks (`eval`, `script.src`, ...) outright. It is a deliberately separate project: DOMPurify stays a focused sanitizer, and DOMFortify handles the document-wide enforcement layer that is intentionally out of DOMPurify's scope.

## Can I configure DOMPurify?

Yes. The included default configuration values are pretty good already - but you can of course override them. Check out the [`/demos`](https://github.com/cure53/DOMPurify/tree/main/demos) folder to see a bunch of examples on how you can [customize DOMPurify](https://github.com/cure53/DOMPurify/tree/main/demos#what-is-this).

Before you widen the allow-list (`ADD_TAGS`, `ADD_ATTR`, `CUSTOM_ELEMENT_HANDLING`, …) or relax a default, it's worth skimming the [tags and attributes to think twice about](https://github.com/cure53/DOMPurify/wiki/Security-Goals-&-Threat-Model#dangerous-tags-and-attributes-think-twice-before-allow-listing) - a few are dangerous in non-obvious ways.

### General settings

```js
// strip {{ ... }}, ${ ... } and <% ... %> to make output safe for template systems
// be careful please, this mode is not recommended for production usage.
// allowing template parsing in user-controlled HTML is not advised at all.
// only use this mode if there is really no alternative.
const clean = DOMPurify.sanitize(dirty, { SAFE_FOR_TEMPLATES: true });

// change how e.g. comments containing risky HTML characters are treated.
// be very careful, this setting should only be set to `false` if you really only handle
// HTML and nothing else, no SVG, MathML or the like.
// Otherwise, changing from `true` to `false` will lead to XSS in this or some other way.
const clean = DOMPurify.sanitize(dirty, { SAFE_FOR_XML: false });
```

### Control our allow-lists and block-lists

```js
// allow only <b> elements, very strict
const clean = DOMPurify.sanitize(dirty, { ALLOWED_TAGS: ['b'] });

// allow only <b> and <q> with style attributes
const clean = DOMPurify.sanitize(dirty, {
  ALLOWED_TAGS: ['b', 'q'],
  ALLOWED_ATTR: ['style'],
});

// allow all safe HTML elements but neither SVG nor MathML
// note that the USE_PROFILES setting will override the ALLOWED_TAGS setting
// so don't use them together
const clean = DOMPurify.sanitize(dirty, { USE_PROFILES: { html: true } });

// allow all safe SVG elements and SVG Filters, no HTML or MathML
const clean = DOMPurify.sanitize(dirty, {
  USE_PROFILES: { svg: true, svgFilters: true },
});

// allow all safe MathML elements and SVG, but no SVG Filters
const clean = DOMPurify.sanitize(dirty, {
  USE_PROFILES: { mathMl: true, svg: true },
});

// change the default namespace from HTML to something different
const clean = DOMPurify.sanitize(dirty, {
  NAMESPACE: 'http://www.w3.org/2000/svg',
});

// leave all safe HTML as it is and add <style> elements to block-list
const clean = DOMPurify.sanitize(dirty, { FORBID_TAGS: ['style'] });

// leave all safe HTML as it is and add style attributes to block-list
const clean = DOMPurify.sanitize(dirty, { FORBID_ATTR: ['style'] });

// extend the existing array of allowed tags and add <my-tag> to allow-list
const clean = DOMPurify.sanitize(dirty, { ADD_TAGS: ['my-tag'] });

// extend the existing array of allowed attributes and add my-attr to allow-list
const clean = DOMPurify.sanitize(dirty, { ADD_ATTR: ['my-attr'] });

// use functions to control which additional tags and attributes are allowed
const allowlist = {
  one: ['attribute-one'],
  two: ['attribute-two'],
};
const clean = DOMPurify.sanitize(
  '<one attribute-one="1" attribute-two="2"></one><two attribute-one="1" attribute-two="2"></two>',
  {
    ADD_TAGS: (tagName) => {
      return Object.keys(allowlist).includes(tagName);
    },
    ADD_ATTR: (attributeName, tagName) => {
      return allowlist[tagName]?.includes(attributeName) || false;
    },
  }
); // <one attribute-one="1"></one><two attribute-two="2"></two>

// prohibit ARIA attributes, leave other safe HTML as is (default is true)
const clean = DOMPurify.sanitize(dirty, { ALLOW_ARIA_ATTR: false });

// prohibit HTML5 data attributes, leave other safe HTML as is (default is true)
const clean = DOMPurify.sanitize(dirty, { ALLOW_DATA_ATTR: false });
```

### Control behavior relating to Custom Elements

```js
// DOMPurify allows to define rules for Custom Elements. When using the CUSTOM_ELEMENT_HANDLING
// literal, it is possible to define exactly what elements you wish to allow (by default, none are allowed).
//
// The same goes for their attributes. By default, the built-in or configured allow.list is used.
//
// You can use a RegExp literal to specify what is allowed or a predicate, examples for both can be seen below.
// When using a predicate function for attributeNameCheck, it can optionally receive the tagName as a second parameter
// for more granular control over which attributes are allowed for specific elements.
// The default values are very restrictive to prevent accidental XSS bypasses. Handle with great care!

const clean = DOMPurify.sanitize(
  '<foo-bar baz="foobar" forbidden="true"></foo-bar><div is="foo-baz"></div>',
  {
    CUSTOM_ELEMENT_HANDLING: {
      tagNameCheck: null, // no custom elements are allowed
      attributeNameCheck: null, // default / standard attribute allow-list is used
      allowCustomizedBuiltInElements: false, // no customized built-ins allowed
    },
  }
); // <div is=""></div>

const clean = DOMPurify.sanitize(
  '<foo-bar baz="foobar" forbidden="true"></foo-bar><div is="foo-baz"></div>',
  {
    CUSTOM_ELEMENT_HANDLING: {
      tagNameCheck: /^foo-/, // allow all tags starting with "foo-"
      attributeNameCheck: /baz/, // allow all attributes containing "baz"
      allowCustomizedBuiltInElements: true, // customized built-ins are allowed
    },
  }
); // <foo-bar baz="foobar"></foo-bar><div is="foo-baz"></div>

const clean = DOMPurify.sanitize(
  '<foo-bar baz="foobar" forbidden="true"></foo-bar><div is="foo-baz"></div>',
  {
    CUSTOM_ELEMENT_HANDLING: {
      tagNameCheck: (tagName) => tagName.match(/^foo-/), // allow all tags starting with "foo-"
      attributeNameCheck: (attr) => attr.match(/baz/), // allow all containing "baz"
      allowCustomizedBuiltInElements: true, // allow customized built-ins
    },
  }
); // <foo-bar baz="foobar"></foo-bar><div is="foo-baz"></div>

// Example with attributeNameCheck receiving tagName as a second parameter
const clean = DOMPurify.sanitize(
  '<element-one attribute-one="1" attribute-two="2"></element-one><element-two attribute-one="1" attribute-two="2"></element-two>',
  {
    CUSTOM_ELEMENT_HANDLING: {
      tagNameCheck: (tagName) => tagName.match(/^element-(one|two)$/),
      attributeNameCheck: (attr, tagName) => {
        if (tagName === 'element-one') {
          return ['attribute-one'].includes(attr);
        } else if (tagName === 'element-two') {
          return ['attribute-two'].includes(attr);
        } else {
          return false;
        }
      },
      allowCustomizedBuiltInElements: false,
    },
  }
); // <element-one attribute-one="1"></element-one><element-two attribute-two="2"></element-two>
```

### Control behavior relating to URI values

```js
// extend the existing array of elements that can use Data URIs
const clean = DOMPurify.sanitize(dirty, { ADD_DATA_URI_TAGS: ['a', 'area'] });

// extend the existing array of elements that are safe for URI-like values (be careful, XSS risk)
const clean = DOMPurify.sanitize(dirty, { ADD_URI_SAFE_ATTR: ['my-attr'] });
```

### Control permitted attribute values

```js
// allow external protocol handlers in URL attributes (default is false, be careful, XSS risk)
// by default only http, https, ftp, ftps, tel, mailto, callto, sms, cid, xmpp and matrix are allowed.
const clean = DOMPurify.sanitize(dirty, { ALLOW_UNKNOWN_PROTOCOLS: true });

// allow specific protocol handlers in URL attributes via regex (default is false, be careful, XSS risk)
// by default only (protocol-)relative URLs, http, https, ftp, ftps, tel, mailto, callto, sms, cid, xmpp and matrix are allowed.
// Default RegExp: /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|sms|cid|xmpp):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i;
const clean = DOMPurify.sanitize(dirty, {
  ALLOWED_URI_REGEXP:
    /^(?:(?:(?:f|ht)tps?|mailto|tel|callto|sms|cid|xmpp|matrix):|[^a-z]|[a-z+.\-]+(?:[^a-z+.\-:]|$))/i,
});
```

### Influence the return-type

```js
// return a DOM HTMLBodyElement instead of an HTML string (default is false)
const clean = DOMPurify.sanitize(dirty, { RETURN_DOM: true });

// return a DOM DocumentFragment instead of an HTML string (default is false)
const clean = DOMPurify.sanitize(dirty, { RETURN_DOM_FRAGMENT: true });

// use the RETURN_TRUSTED_TYPE flag to turn on Trusted Types support if available
const clean = DOMPurify.sanitize(dirty, { RETURN_TRUSTED_TYPE: true }); // will return a TrustedHTML object instead of a string if possible

// use a provided Trusted Types policy
const clean = DOMPurify.sanitize(dirty, {
  // supplied policy must define createHTML and createScriptURL
  TRUSTED_TYPES_POLICY: trustedTypes.createPolicy('dompurify', {
    createHTML(s) {
      return s;
    },
    createScriptURL(s) {
      return s;
    },
  }),
});

// opt out of DOMPurify's internal `dompurify` Trusted Types policy entirely
// (useful when your CSP `trusted-types` allowlist does not include `dompurify`)
const clean = DOMPurify.sanitize(dirty, { TRUSTED_TYPES_POLICY: null });
```

### Influence how we sanitize

```js
// return entire document including <html> tags (default is false)
const clean = DOMPurify.sanitize(dirty, { WHOLE_DOCUMENT: true });

// disable DOM Clobbering protection on output (default is true, handle with care, minor XSS risks here)
const clean = DOMPurify.sanitize(dirty, { SANITIZE_DOM: false });

// enforce strict DOM Clobbering protection via namespace isolation (default is false)
// when enabled, isolates the namespace of named properties (i.e., `id` and `name` attributes)
// from JS variables by prefixing them with the string `user-content-`
const clean = DOMPurify.sanitize(dirty, { SANITIZE_NAMED_PROPS: true });

// keep an element's content when the element is removed (default is true)
const clean = DOMPurify.sanitize(dirty, { KEEP_CONTENT: false });

// glue elements like style, script or others to document.body and prevent unintuitive browser behavior in several edge-cases (default is false)
const clean = DOMPurify.sanitize(dirty, { FORCE_BODY: true });

// remove all <a> elements under <p> elements that are removed
const clean = DOMPurify.sanitize(dirty, {
  FORBID_CONTENTS: ['a'],
  FORBID_TAGS: ['p'],
});

// extend the default FORBID_CONTENTS list to also remove <a> elements under <p> elements
const clean = DOMPurify.sanitize(dirty, {
  ADD_FORBID_CONTENTS: ['a'],
  FORBID_TAGS: ['p'],
});

// change the parser type so sanitized data is treated as XML and not as HTML, which is the default
const clean = DOMPurify.sanitize(dirty, {
  PARSER_MEDIA_TYPE: 'application/xhtml+xml',
});
```

### Influence where we sanitize

```js
// use the IN_PLACE mode to sanitize a node "in place", which is much faster depending on how you use DOMPurify
const dirty = document.createElement('a');
dirty.setAttribute('href', 'javascript:alert(1)');

const clean = DOMPurify.sanitize(dirty, { IN_PLACE: true }); // see https://github.com/cure53/DOMPurify/issues/288 for more info
```

There is even [more examples here](https://github.com/cure53/DOMPurify/tree/main/demos#what-is-this), showing how you can run, customize and configure DOMPurify to fit your needs.

## Persistent Configuration

Instead of repeatedly passing the same configuration to `DOMPurify.sanitize`, you can use the `DOMPurify.setConfig` method. Your configuration will persist until your next call to `DOMPurify.setConfig`, or until you invoke `DOMPurify.clearConfig` to reset it. Remember that there is only one active configuration, which means once it is set, all extra configuration parameters passed to `DOMPurify.sanitize` are ignored.

## Hooks

DOMPurify allows you to augment its functionality by attaching one or more functions with the `DOMPurify.addHook` method to one of the following hooks:

- `beforeSanitizeElements`
- `uponSanitizeElement` (No 's' - called for every element)
- `afterSanitizeElements`
- `beforeSanitizeAttributes`
- `uponSanitizeAttribute`
- `afterSanitizeAttributes`
- `beforeSanitizeShadowDOM`
- `uponSanitizeShadowNode`
- `afterSanitizeShadowDOM`

It passes the currently processed DOM node, when needed a literal with verified node and attribute data and the DOMPurify configuration to the callback. Check out the [MentalJS hook demo](https://github.com/cure53/DOMPurify/blob/main/demos/hooks-mentaljs-demo.html) to see how the API can be used nicely.

_Example_:

```js
DOMPurify.addHook(
  'uponSanitizeAttribute',
  function (currentNode, hookEvent, config) {
    // Do something with the current node
    // You can also mutate hookEvent for current node (i.e. set hookEvent.forceKeepAttr = true)
    // For other than 'uponSanitizeAttribute' hook types hookEvent equals to null
  }
);
```

## Removed Configuration

| Option          | Since | Note                     |
| --------------- | ----- | ------------------------ |
| SAFE_FOR_JQUERY | 2.1.0 | No replacement required. |

## Continuous Integration

We are currently using GitHub Actions in combination with Playwright. This lets us confirm on every commit that everything works in the relevant modern browsers, and a separate scheduled and on-merge workflow re-runs the suite on older engine snapshots so breakage on outdated browsers is caught too. Check out the build logs here: https://github.com/cure53/DOMPurify/actions

You can further run local tests by executing `npm run test`.

All relevant commits will be signed with the key `0x24BB6BF4` for additional security (since 8th of April 2016).

### Development and contributing

#### Installation (`npm i`)

We support `npm` officially. GitHub Actions workflow is configured to install dependencies using `npm`. When using a deprecated version of `npm`, we cannot fully ensure the versions of installed dependencies, which might lead to unanticipated problems.

#### Scripts

We use ESLint via `xo` as part of our pre-commit workflow to help ensure code consistency. In addition, we use [Prettier](https://github.com/prettier/prettier) for source and Markdown formatting, and `/dist` assets are built through `rollup`.

These are our npm scripts:

- `npm run dev` to build the unminified UMD bundle while watching sources for changes
- `npm run test` to lint the sources, run tests through jsdom, and run browser tests in Chromium via Playwright
  - `npm run test:jsdom` to only run tests through jsdom
  - `npm run test:happydom` to run the suite through happy-dom (an unsupported environment; kept as a robustness check, not a compatibility promise)
  - `npm run test:browser` to only run tests through Playwright
  - `npm run test:browser:legacy` to run the suite on older browser engines (point `PW_MODULE` at a pinned old Playwright install; see `.github/workflows/legacy-browsers.yml`)
  - `npm run test:ci` to run the CI test flow for jsdom and Playwright
  - `npm run test:fuzz` to run a small fuzzer covering `sanitize()` and CONFIG
- `npm run bench` to run the jsdom micro-benchmark over the built `dist/purify.cjs` (build first; `--json` and `--compare a.json b.json` support A/B runs across branches - results are directional, confirm user-facing claims in real browsers)
- `npm run coverage` to build an instrumented bundle, run the jsdom suite, and write a local HTML line/branch coverage report to `coverage/index.html` (jsdom scope only, not run in CI)
  - `npm run build:cov` to only build the instrumented coverage bundle
- `npm run lint` to lint the sources using ESLint via xo
- `npm run format` to format JavaScript/TypeScript and Markdown sources with Prettier
  - `npm run format:js` to only format JavaScript/TypeScript sources
  - `npm run format:md` to only format Markdown files
- `npm run build` to build type declarations and distribution bundles, then fix and clean up generated types
  - `npm run build:types` to only emit TypeScript declaration files
  - `npm run build:rollup` to build all Rollup bundles
  - `npm run build:umd` to only build an unminified UMD bundle
  - `npm run build:umd:min` to only build a minified UMD bundle
  - `npm run build:es` to only build the ES module bundle
  - `npm run build:cjs` to only build the CommonJS bundle
  - `npm run build:fix-types` to post-process generated type files
  - `npm run build:cleanup` to clean up temporary generated type output
- `npm run verify-typescript` to run the TypeScript verification script
- `npm run commit-amend-build` to run the maintainer helper script for amending build output

Note: all run scripts triggered via `npm run <script>`.

There are more npm scripts but they are mainly to integrate with CI or are meant to be "private" for instance to amend build distribution files with every commit.

## Security Mailing List

We maintain a mailing list that notifies whenever a **security-critical** release of DOMPurify was published. This means, if someone found a bypass and we fixed it with a release (which always happens when a bypass was found) a mail will go out to that list. This usually happens within minutes or a few hours after learning about a bypass. The list can be subscribed to here:

[https://lists.ruhr-uni-bochum.de/mailman/listinfo/dompurify-security](https://lists.ruhr-uni-bochum.de/mailman/listinfo/dompurify-security)

Feature releases will not be announced to this list.

## Who contributed?

Many people have helped DOMPurify become what it is today, and they deserve to be acknowledged!

[offset](https://github.com/offset), [Bankde](https://github.com/Bankde), [lukewarlow](https://github.com/lukewarlow), [DEMON1A](https://github.com/DEMON1A), [fg0x0](https://github.com/fg0x0), [kodareef5](https://github.com/kodareef5), [DavidOliver](https://github.com/DavidOliver), [1Jesper1](https://github.com/1Jesper1), [bencalif](https://github.com/bencalif), [trace37labs](https://github.com/trace37labs), [eddieran](https://github.com/eddieran), [christos-eth](https://github.com/christos-eth), [researchatfluidattacks](https://github.com/researchatfluidattacks), [frevadiscor](https://github.com/frevadiscor), [Rotzbua](https://github.com/Rotzbua), [binhpv](https://github.com/binhpv), [MariusRumpf](https://github.com/MariusRumpf), [prasadrajandran](https://github.com/prasadrajandran), [Cybozu 💛💸](https://github.com/cybozu), [hata6502 💸](https://github.com/hata6502), [openclaw 💸](https://github.com/openclaw), [intra-mart-dh 💸](https://github.com/intra-mart-dh), [nelstrom ❤️](https://github.com/nelstrom), [hash_kitten ❤️](https://twitter.com/hash_kitten), [kevin_mizu ❤️](https://twitter.com/kevin_mizu), [icesfont ❤️](https://github.com/icesfont), [reduckted ❤️](https://github.com/reduckted), [dcramer 💸](https://github.com/dcramer), [JGraph 💸](https://github.com/jgraph), [baekilda 💸](https://github.com/baekilda), [Healthchecks 💸](https://github.com/healthchecks), [Sentry 💸](https://github.com/getsentry), [jarrodldavis 💸](https://github.com/jarrodldavis), [CynegeticIO](https://github.com/CynegeticIO), [ssi02014 ❤️](https://github.com/ssi02014), [GrantGryczan](https://github.com/GrantGryczan), [Lowdefy](https://twitter.com/lowdefy), [granlem](https://twitter.com/MaximeVeit), [oreoshake](https://github.com/oreoshake), [tdeekens ❤️](https://github.com/tdeekens), [peernohell ❤️](https://github.com/peernohell), [is2ei](https://github.com/is2ei), [SoheilKhodayari](https://github.com/SoheilKhodayari), [franktopel](https://github.com/franktopel), [NateScarlet](https://github.com/NateScarlet), [neilj](https://github.com/neilj), [fhemberger](https://github.com/fhemberger), [Joris-van-der-Wel](https://github.com/Joris-van-der-Wel), [ydaniv](https://github.com/ydaniv), [terjanq](https://twitter.com/terjanq), [filedescriptor](https://github.com/filedescriptor), [ConradIrwin](https://github.com/ConradIrwin), [gibson042](https://github.com/gibson042), [choumx](https://github.com/choumx), [0xSobky](https://github.com/0xSobky), [styfle](https://github.com/styfle), [koto](https://github.com/koto), [tlau88](https://github.com/tlau88), [strugee](https://github.com/strugee), [oparoz](https://github.com/oparoz), [mathiasbynens](https://github.com/mathiasbynens), [edg2s](https://github.com/edg2s), [dnkolegov](https://github.com/dnkolegov), [dhardtke](https://github.com/dhardtke), [wirehead](https://github.com/wirehead), [thorn0](https://github.com/thorn0), [styu](https://github.com/styu), [mozfreddyb ❤️](https://github.com/mozfreddyb), [mikesamuel](https://github.com/mikesamuel), [jorangreef](https://github.com/jorangreef), [jimmyhchan](https://github.com/jimmyhchan), [jameydeorio](https://github.com/jameydeorio), [jameskraus](https://github.com/jameskraus), [hyderali](https://github.com/hyderali), [hansottowirtz](https://github.com/hansottowirtz), [hackvertor](https://github.com/hackvertor), [freddyb](https://github.com/freddyb), [flavorjones](https://github.com/flavorjones), [djfarrelly](https://github.com/djfarrelly), [devd](https://github.com/devd), [camerondunford](https://github.com/camerondunford), [buu700](https://github.com/buu700), [buildog](https://github.com/buildog), [alabiaga](https://github.com/alabiaga), [Vector919](https://github.com/Vector919), [Robbert](https://github.com/Robbert), [GreLI](https://github.com/GreLI), [FuzzySockets](https://github.com/FuzzySockets), [ArtemBernatskyy](https://github.com/ArtemBernatskyy), [@garethheyes](https://twitter.com/garethheyes), [@shafigullin](https://twitter.com/shafigullin), [@mmrupp](https://twitter.com/mmrupp), [@irsdl](https://twitter.com/irsdl),[ShikariSenpai](https://github.com/ShikariSenpai), [ansjdnakjdnajkd](https://github.com/ansjdnakjdnajkd), [@asutherland](https://twitter.com/asutherland), [@mathias](https://twitter.com/mathias), [@cgvwzq](https://twitter.com/cgvwzq), [@robbertatwork](https://twitter.com/robbertatwork), [@giutro](https://twitter.com/giutro), [@CmdEngineer\_](https://twitter.com/CmdEngineer_), [@avr4mit](https://twitter.com/avr4mit), [davecardwell](https://github.com/davecardwell), [Develop-KIM](https://github.com/Develop-KIM), [asamuzaK](https://github.com/asamuzaK), [fishjojo1](https://github.com/fishjojo1), [Rikuxx0](https://github.com/Rikuxx0) and especially [@securitymb ❤️](https://twitter.com/securitymb) & [@masatokinugawa ❤️](https://twitter.com/masatokinugawa)
