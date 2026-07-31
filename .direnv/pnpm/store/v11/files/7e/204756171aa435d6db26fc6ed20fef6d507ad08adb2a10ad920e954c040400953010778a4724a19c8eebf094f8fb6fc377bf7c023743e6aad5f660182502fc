"use strict";

const xpath = require("../level3/xpath");

// This object defines the mapping between the interface name and the generated interface wrapper code.
// Note: The mapping needs to stay as-is in order due to interface evaluation.
// We cannot "refactor" this to something less duplicative because that would break bundlers which depend on static
// analysis of require()s.
const generatedInterfaces = {
  DOMException: require("../../generated/idl/DOMException.js"),

  URL: require("whatwg-url/webidl2js-wrapper").URL,
  URLSearchParams: require("whatwg-url/webidl2js-wrapper").URLSearchParams,

  EventTarget: require("../../generated/idl/EventTarget"),

  NamedNodeMap: require("../../generated/idl/NamedNodeMap"),
  Node: require("../../generated/idl/Node"),
  Attr: require("../../generated/idl/Attr"),
  Element: require("../../generated/idl/Element"),
  DocumentFragment: require("../../generated/idl/DocumentFragment"),
  DOMImplementation: require("../../generated/idl/DOMImplementation"),
  Document: require("../../generated/idl/Document"),
  XMLDocument: require("../../generated/idl/XMLDocument"),
  CharacterData: require("../../generated/idl/CharacterData"),
  Text: require("../../generated/idl/Text"),
  CDATASection: require("../../generated/idl/CDATASection"),
  ProcessingInstruction: require("../../generated/idl/ProcessingInstruction"),
  Comment: require("../../generated/idl/Comment"),
  DocumentType: require("../../generated/idl/DocumentType"),
  NodeList: require("../../generated/idl/NodeList"),
  RadioNodeList: require("../../generated/idl/RadioNodeList"),
  HTMLCollection: require("../../generated/idl/HTMLCollection"),
  HTMLOptionsCollection: require("../../generated/idl/HTMLOptionsCollection"),
  DOMStringMap: require("../../generated/idl/DOMStringMap"),
  DOMTokenList: require("../../generated/idl/DOMTokenList"),

  StyleSheet: require("../../generated/idl/StyleSheet.js"),
  StyleSheetList: require("../../generated/idl/StyleSheetList.js"),
  MediaList: require("../../generated/idl/MediaList.js"),
  CSSRuleList: require("../../generated/idl/CSSRuleList.js"),
  CSSRule: require("../../generated/idl/CSSRule.js"),
  CSSStyleDeclaration: require("../../generated/idl/CSSStyleDeclaration.js"),
  CSSStyleProperties: require("../../generated/idl/CSSStyleProperties.js"),
  CSSStyleSheet: require("../../generated/idl/CSSStyleSheet.js"),
  CSSGroupingRule: require("../../generated/idl/CSSGroupingRule.js"),
  CSSConditionRule: require("../../generated/idl/CSSConditionRule.js"),
  CSSStyleRule: require("../../generated/idl/CSSStyleRule.js"),
  CSSMediaRule: require("../../generated/idl/CSSMediaRule.js"),
  CSSImportRule: require("../../generated/idl/CSSImportRule.js"),
  CSSContainerRule: require("../../generated/idl/CSSContainerRule.js"),
  CSSSupportsRule: require("../../generated/idl/CSSSupportsRule.js"),
  CSSScopeRule: require("../../generated/idl/CSSScopeRule.js"),
  CSSLayerBlockRule: require("../../generated/idl/CSSLayerBlockRule.js"),
  CSSPageRule: require("../../generated/idl/CSSPageRule.js"),
  CSSCounterStyleRule: require("../../generated/idl/CSSCounterStyleRule.js"),
  CSSFontFaceRule: require("../../generated/idl/CSSFontFaceRule.js"),
  CSSKeyframesRule: require("../../generated/idl/CSSKeyframesRule.js"),
  CSSKeyframeRule: require("../../generated/idl/CSSKeyframeRule.js"),
  CSSLayerStatementRule: require("../../generated/idl/CSSLayerStatementRule.js"),
  CSSNamespaceRule: require("../../generated/idl/CSSNamespaceRule.js"),
  CSSNestedDeclarations: require("../../generated/idl/CSSNestedDeclarations.js"),

  HTMLElement: require("../../generated/idl/HTMLElement.js"),
  HTMLHeadElement: require("../../generated/idl/HTMLHeadElement.js"),
  HTMLTitleElement: require("../../generated/idl/HTMLTitleElement.js"),
  HTMLBaseElement: require("../../generated/idl/HTMLBaseElement.js"),
  HTMLLinkElement: require("../../generated/idl/HTMLLinkElement.js"),
  HTMLMetaElement: require("../../generated/idl/HTMLMetaElement.js"),
  HTMLStyleElement: require("../../generated/idl/HTMLStyleElement.js"),
  HTMLBodyElement: require("../../generated/idl/HTMLBodyElement.js"),
  HTMLHeadingElement: require("../../generated/idl/HTMLHeadingElement.js"),
  HTMLParagraphElement: require("../../generated/idl/HTMLParagraphElement.js"),
  HTMLHRElement: require("../../generated/idl/HTMLHRElement.js"),
  HTMLPreElement: require("../../generated/idl/HTMLPreElement.js"),
  HTMLUListElement: require("../../generated/idl/HTMLUListElement.js"),
  HTMLOListElement: require("../../generated/idl/HTMLOListElement.js"),
  HTMLLIElement: require("../../generated/idl/HTMLLIElement.js"),
  HTMLMenuElement: require("../../generated/idl/HTMLMenuElement.js"),
  HTMLDListElement: require("../../generated/idl/HTMLDListElement.js"),
  HTMLDivElement: require("../../generated/idl/HTMLDivElement.js"),
  HTMLAnchorElement: require("../../generated/idl/HTMLAnchorElement.js"),
  HTMLAreaElement: require("../../generated/idl/HTMLAreaElement.js"),
  HTMLBRElement: require("../../generated/idl/HTMLBRElement.js"),
  HTMLButtonElement: require("../../generated/idl/HTMLButtonElement.js"),
  HTMLCanvasElement: require("../../generated/idl/HTMLCanvasElement.js"),
  HTMLDataElement: require("../../generated/idl/HTMLDataElement.js"),
  HTMLDataListElement: require("../../generated/idl/HTMLDataListElement.js"),
  HTMLDetailsElement: require("../../generated/idl/HTMLDetailsElement.js"),
  HTMLDialogElement: require("../../generated/idl/HTMLDialogElement.js"),
  HTMLDirectoryElement: require("../../generated/idl/HTMLDirectoryElement.js"),
  HTMLFieldSetElement: require("../../generated/idl/HTMLFieldSetElement.js"),
  HTMLFontElement: require("../../generated/idl/HTMLFontElement.js"),
  HTMLFormElement: require("../../generated/idl/HTMLFormElement.js"),
  HTMLHtmlElement: require("../../generated/idl/HTMLHtmlElement.js"),
  HTMLImageElement: require("../../generated/idl/HTMLImageElement.js"),
  HTMLInputElement: require("../../generated/idl/HTMLInputElement.js"),
  HTMLLabelElement: require("../../generated/idl/HTMLLabelElement.js"),
  HTMLLegendElement: require("../../generated/idl/HTMLLegendElement.js"),
  HTMLMapElement: require("../../generated/idl/HTMLMapElement.js"),
  HTMLMarqueeElement: require("../../generated/idl/HTMLMarqueeElement.js"),
  HTMLMediaElement: require("../../generated/idl/HTMLMediaElement.js"),
  HTMLMeterElement: require("../../generated/idl/HTMLMeterElement.js"),
  HTMLModElement: require("../../generated/idl/HTMLModElement.js"),
  HTMLOptGroupElement: require("../../generated/idl/HTMLOptGroupElement.js"),
  HTMLOptionElement: require("../../generated/idl/HTMLOptionElement.js"),
  HTMLOutputElement: require("../../generated/idl/HTMLOutputElement.js"),
  HTMLPictureElement: require("../../generated/idl/HTMLPictureElement.js"),
  HTMLProgressElement: require("../../generated/idl/HTMLProgressElement.js"),
  HTMLQuoteElement: require("../../generated/idl/HTMLQuoteElement.js"),
  HTMLScriptElement: require("../../generated/idl/HTMLScriptElement.js"),
  HTMLSelectElement: require("../../generated/idl/HTMLSelectElement.js"),
  HTMLSlotElement: require("../../generated/idl/HTMLSlotElement.js"),
  HTMLSourceElement: require("../../generated/idl/HTMLSourceElement.js"),
  HTMLSpanElement: require("../../generated/idl/HTMLSpanElement.js"),
  HTMLTableCaptionElement: require("../../generated/idl/HTMLTableCaptionElement.js"),
  HTMLTableCellElement: require("../../generated/idl/HTMLTableCellElement.js"),
  HTMLTableColElement: require("../../generated/idl/HTMLTableColElement.js"),
  HTMLTableElement: require("../../generated/idl/HTMLTableElement.js"),
  HTMLTimeElement: require("../../generated/idl/HTMLTimeElement.js"),
  HTMLTableRowElement: require("../../generated/idl/HTMLTableRowElement.js"),
  HTMLTableSectionElement: require("../../generated/idl/HTMLTableSectionElement.js"),
  HTMLTemplateElement: require("../../generated/idl/HTMLTemplateElement.js"),
  HTMLTextAreaElement: require("../../generated/idl/HTMLTextAreaElement.js"),
  HTMLUnknownElement: require("../../generated/idl/HTMLUnknownElement.js"),
  HTMLFrameElement: require("../../generated/idl/HTMLFrameElement.js"),
  HTMLFrameSetElement: require("../../generated/idl/HTMLFrameSetElement.js"),
  HTMLIFrameElement: require("../../generated/idl/HTMLIFrameElement.js"),
  HTMLEmbedElement: require("../../generated/idl/HTMLEmbedElement.js"),
  HTMLObjectElement: require("../../generated/idl/HTMLObjectElement.js"),
  HTMLParamElement: require("../../generated/idl/HTMLParamElement.js"),
  HTMLVideoElement: require("../../generated/idl/HTMLVideoElement.js"),
  HTMLAudioElement: require("../../generated/idl/HTMLAudioElement.js"),
  HTMLTrackElement: require("../../generated/idl/HTMLTrackElement.js"),
  HTMLFormControlsCollection: require("../../generated/idl/HTMLFormControlsCollection.js"),

  SVGElement: require("../../generated/idl/SVGElement.js"),
  SVGGraphicsElement: require("../../generated/idl/SVGGraphicsElement.js"),
  SVGSVGElement: require("../../generated/idl/SVGSVGElement.js"),
  SVGGElement: require("../../generated/idl/SVGGElement.js"),
  SVGDefsElement: require("../../generated/idl/SVGDefsElement.js"),
  SVGDescElement: require("../../generated/idl/SVGDescElement.js"),
  SVGMetadataElement: require("../../generated/idl/SVGMetadataElement.js"),
  SVGTitleElement: require("../../generated/idl/SVGTitleElement.js"),
  SVGSymbolElement: require("../../generated/idl/SVGSymbolElement.js"),
  SVGSwitchElement: require("../../generated/idl/SVGSwitchElement.js"),

  SVGAnimatedPreserveAspectRatio: require("../../generated/idl/SVGAnimatedPreserveAspectRatio"),
  SVGAnimatedRect: require("../../generated/idl/SVGAnimatedRect"),
  SVGAnimatedString: require("../../generated/idl/SVGAnimatedString"),
  SVGNumber: require("../../generated/idl/SVGNumber"),
  SVGPreserveAspectRatio: require("../../generated/idl/SVGPreserveAspectRatio"),
  SVGRect: require("../../generated/idl/SVGRect"),
  SVGStringList: require("../../generated/idl/SVGStringList"),

  Event: require("../../generated/idl/Event"),
  BeforeUnloadEvent: require("../../generated/idl/BeforeUnloadEvent"),
  BlobEvent: require("../../generated/idl/BlobEvent"),
  CloseEvent: require("../../generated/idl/CloseEvent"),
  CustomEvent: require("../../generated/idl/CustomEvent"),
  DeviceOrientationEvent: require("../../generated/idl/DeviceOrientationEvent"),
  DeviceMotionEvent: require("../../generated/idl/DeviceMotionEvent"),
  ErrorEvent: require("../../generated/idl/ErrorEvent"),
  HashChangeEvent: require("../../generated/idl/HashChangeEvent"),
  MessageEvent: require("../../generated/idl/MessageEvent"),
  PageTransitionEvent: require("../../generated/idl/PageTransitionEvent"),
  PopStateEvent: require("../../generated/idl/PopStateEvent"),
  PromiseRejectionEvent: require("../../generated/idl/PromiseRejectionEvent"),
  ProgressEvent: require("../../generated/idl/ProgressEvent"),
  StorageEvent: require("../../generated/idl/StorageEvent"),
  SubmitEvent: require("../../generated/idl/SubmitEvent"),
  TransitionEvent: require("../../generated/idl/TransitionEvent"),

  UIEvent: require("../../generated/idl/UIEvent"),
  FocusEvent: require("../../generated/idl/FocusEvent"),
  InputEvent: require("../../generated/idl/InputEvent"),
  MouseEvent: require("../../generated/idl/MouseEvent"),
  PointerEvent: require("../../generated/idl/PointerEvent"),
  KeyboardEvent: require("../../generated/idl/KeyboardEvent"),
  TouchEvent: require("../../generated/idl/TouchEvent"),
  CompositionEvent: require("../../generated/idl/CompositionEvent"),
  WheelEvent: require("../../generated/idl/WheelEvent"),

  BarProp: require("../../generated/idl/BarProp"),
  External: require("../../generated/idl/External"),
  Location: require("../../generated/idl/Location"),
  History: require("../../generated/idl/History"),
  Screen: require("../../generated/idl/Screen"),
  Performance: require("../../generated/idl/Performance"),
  Navigator: require("../../generated/idl/Navigator"),

  Crypto: require("../../generated/idl/Crypto"),

  PluginArray: require("../../generated/idl/PluginArray"),
  MimeTypeArray: require("../../generated/idl/MimeTypeArray"),
  Plugin: require("../../generated/idl/Plugin"),
  MimeType: require("../../generated/idl/MimeType"),

  FileReader: require("../../generated/idl/FileReader"),
  Blob: require("../../generated/idl/Blob"),
  File: require("../../generated/idl/File"),
  FileList: require("../../generated/idl/FileList"),
  ValidityState: require("../../generated/idl/ValidityState"),

  DOMParser: require("../../generated/idl/DOMParser"),
  XMLSerializer: require("../../generated/idl/XMLSerializer"),

  FormData: require("../../generated/idl/FormData"),
  XMLHttpRequestEventTarget: require("../../generated/idl/XMLHttpRequestEventTarget"),
  XMLHttpRequestUpload: require("../../generated/idl/XMLHttpRequestUpload"),
  XMLHttpRequest: require("../../generated/idl/XMLHttpRequest"),
  WebSocket: require("../../generated/idl/WebSocket"),

  NodeFilter: require("../../generated/idl/NodeFilter"),
  NodeIterator: require("../../generated/idl/NodeIterator"),
  TreeWalker: require("../../generated/idl/TreeWalker"),

  AbstractRange: require("../../generated/idl/AbstractRange"),
  Range: require("../../generated/idl/Range"),
  StaticRange: require("../../generated/idl/StaticRange"),
  Selection: require("../../generated/idl/Selection"),

  Storage: require("../../generated/idl/Storage"),

  CustomElementRegistry: require("../../generated/idl/CustomElementRegistry"),
  ElementInternals: require("../../generated/idl/ElementInternals"),
  ShadowRoot: require("../../generated/idl/ShadowRoot"),

  MutationObserver: require("../../generated/idl/MutationObserver"),
  MutationRecord: require("../../generated/idl/MutationRecord"),

  Headers: require("../../generated/idl/Headers"),
  AbortController: require("../../generated/idl/AbortController"),
  AbortSignal: require("../../generated/idl/AbortSignal"),

  DeviceMotionEventAcceleration: require("../../generated/idl/DeviceMotionEventAcceleration"),
  DeviceMotionEventRotationRate: require("../../generated/idl/DeviceMotionEventRotationRate"),

  DOMRectReadOnly: require("../../generated/idl/DOMRectReadOnly"),
  DOMRect: require("../../generated/idl/DOMRect"),

  TextDecoder: require("../../generated/idl/TextDecoder"),
  TextEncoder: require("../../generated/idl/TextEncoder")
};

function install(window, name, interfaceConstructor) {
  Object.defineProperty(window, name, {
    configurable: true,
    writable: true,
    value: interfaceConstructor
  });
}

exports.installInterfaces = (window, globalNames) => {
  // Install generated interface.
  for (const generatedInterface of Object.values(generatedInterfaces)) {
    generatedInterface.install(window, globalNames);
  }

  // Install legacy HTMLDocument interface
  // https://html.spec.whatwg.org/#htmldocument
  install(window, "HTMLDocument", window.Document);

  // https://webidl.spec.whatwg.org/#es-DOMException-specialness
  Object.setPrototypeOf(window.DOMException.prototype, window.Error.prototype);

  xpath(window);
};

// Returns an interface webidl2js wrapper given its an interface name.
exports.getInterfaceWrapper = name => {
  return generatedInterfaces[name];
};
