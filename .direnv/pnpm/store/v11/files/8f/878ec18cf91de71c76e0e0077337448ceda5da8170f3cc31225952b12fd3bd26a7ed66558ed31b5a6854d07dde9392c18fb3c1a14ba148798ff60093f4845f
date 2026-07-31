"use strict";
const { legacyHookDecode } = require("@exodus/bytes/encoding.js");
const whatwgURL = require("whatwg-url");
const { parseStyleSheet, parseIntoStyleSheet } = require("./css-parser");

// TODO: this should really implement https://html.spec.whatwg.org/multipage/links.html#link-type-stylesheet
// It (and the things it calls) is nowhere close right now.
exports.fetchStyleSheet = (elementImpl, urlString, importRule) => {
  const document = elementImpl._ownerDocument;
  let defaultEncodingLabel = document._encoding;
  const resourceLoader = document._resourceLoader;

  if (elementImpl.localName === "link" && elementImpl.hasAttributeNS(null, "charset")) {
    defaultEncodingLabel = elementImpl.getAttributeNS(null, "charset");
  }

  function onStyleSheetLoad(data, response) {
    if (!response.ok) {
      throw new Error("Status code: " + response.status);
    }

    // if the element was detached before the load could finish, don't process the data
    if (!elementImpl._attached) {
      return;
    }

    const css = legacyHookDecode(data, defaultEncodingLabel);

    // TODO: MIME type checking?
    if (!importRule && elementImpl.sheet) {
      exports.removeStyleSheet(elementImpl.sheet, elementImpl);
    }

    if (importRule) {
      parseImportedStyleSheet(css, importRule);
    } else {
      const createdSheet = exports.createStyleSheetForElement(css, elementImpl, urlString);
      exports.addStyleSheet(createdSheet, elementImpl);
    }
  }

  resourceLoader.fetch(urlString, {
    element: elementImpl,
    onLoad: onStyleSheetLoad
  });
};

// https://drafts.csswg.org/cssom/#remove-a-css-style-sheet
exports.removeStyleSheet = (sheet, elementImpl) => {
  const { styleSheets } = elementImpl._ownerDocument;
  styleSheets._remove(sheet);

  // Remove the association explicitly; in the spec it's implicit so this step doesn't exist.
  elementImpl.sheet = null;

  elementImpl._ownerDocument._clearStyleCache();

  sheet.parentStyleSheet = null;
  sheet.ownerNode = null;
  sheet.ownerRule = null;
};

// https://drafts.csswg.org/cssom/#create-a-css-style-sheet kinda:
// - Parsing failures are now handled gracefully
// - Like the browser's behaviour, when css-tree encounters invalid CSS
//   it will try its best to ignore the invalid parts without blocking the parsing operations
//   returning a stylesheet with the valid parts only.
// - The import rules stuff seems out of place, and probably should affect the load event...
exports.createStyleSheetForElement = (cssText, elementImpl, href) => {
  const globalObject = elementImpl._globalObject;
  const baseURLString = href ?? elementImpl._ownerDocument.baseURLSerialized();

  const sheetImpl = parseStyleSheet(cssText, globalObject, {
    href,
    ownerNode: elementImpl,
    mediaText: elementImpl.getAttributeNS(null, "media") || "",
    title: elementImpl.getAttributeNS(null, "title"),
    onError(err) {
      const error = new Error("Could not parse CSS stylesheet", { cause: err });
      error.sheetText = cssText;
      error.type = "css-parsing";

      globalObject._virtualConsole.emit("jsdomError", error);
    }
  });

  scanForImportRules(globalObject, sheetImpl.cssRules._list, baseURLString);

  return sheetImpl;
};

function parseImportedStyleSheet(cssText, importRule) {
  const globalObject = importRule._globalObject;
  const baseURLString = importRule.styleSheet.href;

  parseIntoStyleSheet(cssText, globalObject, importRule.styleSheet, err => {
    const error = new Error("Could not parse CSS stylesheet", { cause: err });
    error.sheetText = cssText;
    error.type = "css-parsing";

    globalObject._virtualConsole.emit("jsdomError", error);
  });

  scanForImportRules(globalObject, importRule.styleSheet.cssRules._list, baseURLString);
}

// https://drafts.csswg.org/cssom/#add-a-css-style-sheet
exports.addStyleSheet = (sheet, elementImpl) => {
  elementImpl._ownerDocument.styleSheets._add(sheet);

  // Set the association explicitly; in the spec it's implicit.
  elementImpl.sheet = sheet;

  elementImpl._ownerDocument._clearStyleCache();

  // TODO: title and disabled stuff
};

// Tracking in https://github.com/jsdom/jsdom/issues/2124
function scanForImportRules(globalObject, ruleImpls, baseURLString) {
  for (const ruleImpl of ruleImpls) {
    if (ruleImpl.cssRules) {
      // @media rule: keep searching inside it.
      scanForImportRules(globalObject, ruleImpl.cssRules._list, baseURLString);
    } else if (ruleImpl.href) {
      // @import rule: fetch the resource and evaluate it.

      // Use `whatwgURL.URL` instead of `whatwgURL.parseURL()` so that we get an error we can use as the `cause` if the
      // URL doesn't parse. (All resource loading errors in jsdom must have `cause`s.)
      let parsed;
      try {
        parsed = new whatwgURL.URL(ruleImpl.href, baseURLString);
      } catch (cause) {
        const error = new Error(
          `Could not parse CSS @import URL "${ruleImpl.href}" relative to base URL "${baseURLString}"`,
          { cause }
        );
        error.type = "resource-loading";
        error.url = ruleImpl.href;
        globalObject._virtualConsole.emit("jsdomError", error);
      }

      if (parsed) {
        ruleImpl.styleSheet.href = parsed.href;

        // We need the ownerNode element to fetch. Walk up from the sheet to find it.
        let sheet = ruleImpl.parentStyleSheet;
        while (sheet.ownerRule) {
          sheet = sheet.ownerRule.parentStyleSheet;
        }
        exports.fetchStyleSheet(sheet.ownerNode, parsed.href, ruleImpl);
      }
    }
  }
}
