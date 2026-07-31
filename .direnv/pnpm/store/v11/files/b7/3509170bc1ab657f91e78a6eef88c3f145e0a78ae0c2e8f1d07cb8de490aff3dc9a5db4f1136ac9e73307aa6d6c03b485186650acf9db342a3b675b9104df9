import { type DecodingMode } from "./decode.js";
/** The level of entities to support. */
export declare enum EntityLevel {
    /** Support only XML entities. */
    XML = 0,
    /** Support HTML entities, which are a superset of XML entities. */
    HTML = 1
}
/**
 * Encoding strategy used by `encode`.
 */
export declare enum EncodingMode {
    /**
     * The output is UTF-8 encoded. Only characters that need escaping within
     * XML will be escaped.
     */
    UTF8 = 0,
    /**
     * The output consists only of ASCII characters. Characters that need
     * escaping within HTML, and characters that aren't ASCII characters will
     * be escaped.
     */
    ASCII = 1,
    /**
     * Encode all characters that have an equivalent entity, as well as all
     * characters that are not ASCII characters.
     */
    Extensive = 2,
    /**
     * Encode all characters that have to be escaped in HTML attributes,
     * following {@link https://html.spec.whatwg.org/multipage/parsing.html#escapingString}.
     */
    Attribute = 3,
    /**
     * Encode all characters that have to be escaped in HTML text,
     * following {@link https://html.spec.whatwg.org/multipage/parsing.html#escapingString}.
     */
    Text = 4
}
/**
 * Options for `decode`.
 */
export interface DecodingOptions {
    /**
     * The level of entities to support.
     * @default {@link EntityLevel.XML}
     */
    level?: EntityLevel;
    /**
     * Decoding mode. If `Legacy`, will support legacy entities not terminated
     * with a semicolon (`;`).
     *
     * Always `Strict` for XML. For HTML, set this to `true` if you are parsing
     * an attribute value.
     * @default {@link DecodingMode.Legacy}
     */
    mode?: DecodingMode | undefined;
}
/**
 * Decodes a string with entities.
 * @param input String to decode.
 * @param options Decoding options.
 */
export declare function decode(input: string, options?: DecodingOptions | EntityLevel): string;
/**
 * Options for `encode`.
 */
export interface EncodingOptions {
    /**
     * The level of entities to support.
     * @default {@link EntityLevel.XML}
     */
    level?: EntityLevel;
    /**
     * Output format.
     * @default {@link EncodingMode.Extensive}
     */
    mode?: EncodingMode;
}
/**
 * Encodes a string with entities.
 * @param input String to encode.
 * @param options Encoding options.
 */
export declare function encode(input: string, options?: EncodingOptions | EntityLevel): string;
export { DecodingMode, decodeHTML, decodeHTMLAttribute, decodeHTMLStrict, decodeXML, decodeXML as decodeXMLStrict, EntityDecoder, } from "./decode.js";
export { encodeHTML, encodeNonAsciiHTML, } from "./encode.js";
export { encodeXML, escape, escapeAttribute, escapeText, escapeUTF8, } from "./escape.js";
//# sourceMappingURL=index.d.ts.map