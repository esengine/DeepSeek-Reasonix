"use strict";
const path = require("path");
const { pathToFileURL } = require("url");
const fs = require("fs").promises;
const vm = require("vm");
const toughCookie = require("tough-cookie");
const sniffHTMLEncoding = require("html-encoding-sniffer");
const whatwgURL = require("whatwg-url");
const { legacyHookDecode } = require("@exodus/bytes/encoding.js");
const { URL } = require("whatwg-url");
const { MIMEType } = require("whatwg-mimetype");
const { getGlobalDispatcher } = require("undici");
const idlUtils = require("./generated/idl/utils.js");
const VirtualConsole = require("./jsdom/virtual-console.js");
const { createWindow } = require("./jsdom/browser/Window.js");
const { parseIntoDocument } = require("./jsdom/browser/parser");
const { fragmentSerialization } = require("./jsdom/living/domparsing/serialization.js");
const createDecompressInterceptor = require("./jsdom/browser/resources/decompress-interceptor.js");
const {
  JSDOMDispatcher, DEFAULT_USER_AGENT, fetchCollected
} = require("./jsdom/browser/resources/jsdom-dispatcher.js");
const requestInterceptor = require("./jsdom/browser/resources/request-interceptor.js");

class CookieJar extends toughCookie.CookieJar {
  constructor(store, options) {
    // jsdom cookie jars must be loose by default
    super(store, { looseMode: true, ...options });
  }
}

const window = Symbol("window");
let sharedFragmentDocument = null;

class JSDOM {
  constructor(input = "", options = {}) {
    const mimeType = new MIMEType(options.contentType === undefined ? "text/html" : options.contentType);
    const { html, encoding } = normalizeHTML(input, mimeType);

    options = transformOptions(options, encoding, mimeType);

    this[window] = createWindow(options.windowOptions);

    const documentImpl = idlUtils.implForWrapper(this[window]._document);

    options.beforeParse(this[window]._globalProxy);

    parseIntoDocument(html, documentImpl);

    documentImpl.close();
  }

  get window() {
    // It's important to grab the global proxy, instead of just the result of `createWindow(...)`, since otherwise
    // things like `window.eval` don't exist.
    return this[window]._globalProxy;
  }

  get virtualConsole() {
    return this[window]._virtualConsole;
  }

  get cookieJar() {
    // TODO NEWAPI move _cookieJar to window probably
    return idlUtils.implForWrapper(this[window]._document)._cookieJar;
  }

  serialize() {
    return fragmentSerialization(idlUtils.implForWrapper(this[window]._document), { requireWellFormed: false });
  }

  nodeLocation(node) {
    if (!idlUtils.implForWrapper(this[window]._document)._parseOptions.sourceCodeLocationInfo) {
      throw new Error("Location information was not saved for this jsdom. Use includeNodeLocations during creation.");
    }

    return idlUtils.implForWrapper(node).sourceCodeLocation;
  }

  getInternalVMContext() {
    if (!vm.isContext(this[window])) {
      throw new TypeError("This jsdom was not configured to allow script running. " +
        "Use the runScripts option during creation.");
    }

    return this[window];
  }

  reconfigure(settings) {
    if ("windowTop" in settings) {
      this[window]._top = settings.windowTop;
    }

    if ("url" in settings) {
      const document = idlUtils.implForWrapper(this[window]._document);

      const url = whatwgURL.parseURL(settings.url);
      if (url === null) {
        throw new TypeError(`Could not parse "${settings.url}" as a URL`);
      }

      document._URL = url;
      document._origin = whatwgURL.serializeURLOrigin(document._URL);
      this[window]._sessionHistory.currentEntry.url = url;
      document._clearBaseURLCache();
    }
  }

  static fragment(string = "") {
    if (!sharedFragmentDocument) {
      sharedFragmentDocument = (new JSDOM()).window.document;
    }

    const template = sharedFragmentDocument.createElement("template");
    template.innerHTML = string;
    return template.content;
  }

  static async fromURL(url, options = {}) {
    options = normalizeFromURLOptions(options);

    // Build the dispatcher for the initial request
    // For the initial fetch, we default to "usable" instead of no resource loading, since fromURL() implicitly requests
    // fetching the initial resource. This does not impact further resource fetching, which uses options.resources.
    const resourcesForInitialFetch = options.resources !== undefined ? options.resources : "usable";
    const { effectiveDispatcher } = extractResourcesOptions(resourcesForInitialFetch, options.cookieJar);

    const headers = { Accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8" };
    if (options.referrer) {
      headers.Referer = options.referrer;
    }

    const response = await fetchCollected(effectiveDispatcher, {
      url,
      headers
    });

    if (!response.ok) {
      throw new Error(`Resource was not loaded. Status: ${response.status}`);
    }

    options = Object.assign(options, {
      url: response.url,
      contentType: response.headers["content-type"] || undefined,
      referrer: options.referrer,
      resources: options.resources
    });

    return new JSDOM(response.body, options);
  }

  static async fromFile(filename, options = {}) {
    options = normalizeFromFileOptions(filename, options);
    const nodeBuffer = await fs.readFile(filename);

    return new JSDOM(nodeBuffer, options);
  }
}

function normalizeFromURLOptions(options) {
  // Checks on options that are invalid for `fromURL`
  if (options.url !== undefined) {
    throw new TypeError("Cannot supply a url option when using fromURL");
  }
  if (options.contentType !== undefined) {
    throw new TypeError("Cannot supply a contentType option when using fromURL");
  }

  // Normalization of options which must be done before the rest of the fromURL code can use them, because they are
  // given to request()
  const normalized = { ...options };

  if (options.referrer !== undefined) {
    normalized.referrer = (new URL(options.referrer)).href;
  }

  if (options.cookieJar === undefined) {
    normalized.cookieJar = new CookieJar();
  }

  return normalized;

  // All other options don't need to be processed yet, and can be taken care of in the normal course of things when
  // `fromURL` calls `new JSDOM(html, options)`.
}

function extractResourcesOptions(resources, cookieJar) {
  // loadSubresources controls whether PerDocumentResourceLoader fetches scripts, stylesheets, etc.
  // XHR always works regardless of this flag.
  let userAgent, baseDispatcher, userInterceptors, loadSubresources;

  if (resources === undefined) {
    // resources: undefined means no automatic subresource fetching, but XHR still works
    userAgent = DEFAULT_USER_AGENT;
    baseDispatcher = getGlobalDispatcher();
    userInterceptors = [];
    loadSubresources = false;
  } else if (resources === "usable") {
    // resources: "usable" means use all defaults
    userAgent = DEFAULT_USER_AGENT;
    baseDispatcher = getGlobalDispatcher();
    userInterceptors = [];
    loadSubresources = true;
  } else if (typeof resources === "object" && resources !== null) {
    // resources: { userAgent?, dispatcher?, interceptors? }
    userAgent = resources.userAgent !== undefined ? resources.userAgent : DEFAULT_USER_AGENT;
    baseDispatcher = resources.dispatcher !== undefined ? resources.dispatcher : getGlobalDispatcher();
    userInterceptors = resources.interceptors !== undefined ? resources.interceptors : [];
    loadSubresources = true;
  } else {
    throw new TypeError(`resources must be undefined, "usable", or an object`);
  }

  // User interceptors come first (outermost), then decompress interceptor
  const allUserInterceptors = [
    ...userInterceptors,
    createDecompressInterceptor()
  ];

  return {
    userAgent,
    effectiveDispatcher: new JSDOMDispatcher({
      baseDispatcher,
      cookieJar,
      userAgent,
      userInterceptors: allUserInterceptors
    }),
    loadSubresources
  };
}

function normalizeFromFileOptions(filename, options) {
  const normalized = { ...options };

  if (normalized.contentType === undefined) {
    const extname = path.extname(filename);
    if (extname === ".xhtml" || extname === ".xht" || extname === ".xml") {
      normalized.contentType = "application/xhtml+xml";
    }
  }

  if (normalized.url === undefined) {
    normalized.url = pathToFileURL(path.resolve(filename)).href;
  }

  return normalized;
}

function transformOptions(options, encoding, mimeType) {
  const transformed = {
    windowOptions: {
      // Defaults
      url: "about:blank",
      referrer: "",
      contentType: "text/html",
      parsingMode: "html",
      parseOptions: {
        sourceCodeLocationInfo: false,
        scriptingEnabled: false
      },
      runScripts: undefined,
      encoding,
      pretendToBeVisual: false,
      storageQuota: 5000000,

      // Defaults filled in later
      dispatcher: undefined,
      loadSubresources: undefined,
      userAgent: undefined,
      virtualConsole: undefined,
      cookieJar: undefined
    },

    // Defaults
    beforeParse() { }
  };

  // options.contentType was parsed into mimeType by the caller.
  if (!mimeType.isHTML() && !mimeType.isXML()) {
    throw new RangeError(`The given content type of "${options.contentType}" was not a HTML or XML content type`);
  }

  transformed.windowOptions.contentType = mimeType.essence;
  transformed.windowOptions.parsingMode = mimeType.isHTML() ? "html" : "xml";

  if (options.url !== undefined) {
    transformed.windowOptions.url = (new URL(options.url)).href;
  }

  if (options.referrer !== undefined) {
    transformed.windowOptions.referrer = (new URL(options.referrer)).href;
  }

  if (options.includeNodeLocations) {
    if (transformed.windowOptions.parsingMode === "xml") {
      throw new TypeError("Cannot set includeNodeLocations to true with an XML content type");
    }

    transformed.windowOptions.parseOptions = { sourceCodeLocationInfo: true };
  }

  transformed.windowOptions.cookieJar = options.cookieJar === undefined ?
                                       new CookieJar() :
                                       options.cookieJar;

  transformed.windowOptions.virtualConsole = options.virtualConsole === undefined ?
                                            (new VirtualConsole()).forwardTo(console) :
                                            options.virtualConsole;

  if (!(transformed.windowOptions.virtualConsole instanceof VirtualConsole)) {
    throw new TypeError("virtualConsole must be an instance of VirtualConsole");
  }

  const { userAgent, effectiveDispatcher, loadSubresources } =
    extractResourcesOptions(options.resources, transformed.windowOptions.cookieJar);
  transformed.windowOptions.userAgent = userAgent;
  transformed.windowOptions.dispatcher = effectiveDispatcher;
  transformed.windowOptions.loadSubresources = loadSubresources;

  if (options.runScripts !== undefined) {
    transformed.windowOptions.runScripts = String(options.runScripts);
    if (transformed.windowOptions.runScripts === "dangerously") {
      transformed.windowOptions.parseOptions.scriptingEnabled = true;
    } else if (transformed.windowOptions.runScripts !== "outside-only") {
      throw new RangeError(`runScripts must be undefined, "dangerously", or "outside-only"`);
    }
  }

  if (options.beforeParse !== undefined) {
    transformed.beforeParse = options.beforeParse;
  }

  if (options.pretendToBeVisual !== undefined) {
    transformed.windowOptions.pretendToBeVisual = Boolean(options.pretendToBeVisual);
  }

  if (options.storageQuota !== undefined) {
    transformed.windowOptions.storageQuota = Number(options.storageQuota);
  }

  return transformed;
}

function normalizeHTML(html, mimeType) {
  let encoding = "UTF-8";

  if (html instanceof Uint8Array) {
    // leave as-is
  } else if (ArrayBuffer.isView(html)) {
    html = new Uint8Array(html.buffer, html.byteOffset, html.byteLength);
  } else if (html instanceof ArrayBuffer) {
    html = new Uint8Array(html);
  }

  if (html instanceof Uint8Array) {
    encoding = sniffHTMLEncoding(html, {
      xml: mimeType.isXML(),
      transportLayerEncodingLabel: mimeType.parameters.get("charset")
    });
    html = legacyHookDecode(html, encoding);
  } else {
    html = String(html);
  }

  return { html, encoding };
}

exports.JSDOM = JSDOM;

exports.VirtualConsole = VirtualConsole;
exports.CookieJar = CookieJar;
exports.requestInterceptor = requestInterceptor;

exports.toughCookie = toughCookie;
