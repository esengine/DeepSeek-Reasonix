# Determine the Encoding of a HTML Byte Stream

This package implements the HTML Standard's [encoding sniffing algorithm](https://html.spec.whatwg.org/multipage/syntax.html#encoding-sniffing-algorithm) in all its glory. The most interesting part of this is how it pre-scans the first 1024 bytes in order to search for certain `<meta charset>`-related patterns.

```js
const htmlEncodingSniffer = require("html-encoding-sniffer");
const fs = require("fs");

const htmlBytes = fs.readFileSync("./html-page.html");
const sniffedEncoding = htmlEncodingSniffer(htmlBytes);
```

The passed bytes are given as a `Uint8Array`; the Node.js `Buffer` subclass of `Uint8Array` will also work, as shown above.

The returned value will be a canonical [encoding name](https://encoding.spec.whatwg.org/#names-and-labels) (not a label). You might then combine this with the [`@exodus/bytes`](https://github.com/ExodusOSS/bytes/) package to decode the result:

```js
const { TextDecoder } = require("@exodus/bytes");
const htmlString = (new TextDecoder(sniffedEncoding)).decode(htmlBytes);
```

## Options

You can pass the following options to `htmlEncodingSniffer`:

```js
const sniffedEncoding = htmlEncodingSniffer(htmlBytes, {
  xml,
  transportLayerEncodingLabel,
  defaultEncoding,
});
```

The `xml` option is a boolean, defaulting to `false`. If set to `true`, then we bypass the [HTML encoding sniffing algorithm](https://html.spec.whatwg.org/multipage/syntax.html#encoding-sniffing-algorithm) and compute the encoding based on the presence of a BOM, or the other options provided. (In the future, we may perform sniffing of the `<?xml?>` declaration, but for now that is not implemented.)

The `transportLayerEncodingLabel` is an encoding label that is obtained from the "transport layer" (probably a HTTP `Content-Type` header), which overrides everything but a BOM.

The `defaultEncoding` is the ultimate fallback encoding used if no valid encoding is supplied by the transport layer, and no encoding is sniffed from the bytes. For HTML, it defaults to `"windows-1252"`, as recommended by the algorithm's table of suggested defaults for "All other locales" (including the `en` locale). For XML, it defaults to `"UTF-8"`.

## Credits

This package was originally based on the excellent work of [@nicolashenry](https://github.com/nicolashenry), [in jsdom](https://github.com/tmpvar/jsdom/blob/16fd85618f2705d181232f6552125872a37164bc/lib/jsdom/living/helpers/encoding.js). It has since been pulled out into this separate package.
