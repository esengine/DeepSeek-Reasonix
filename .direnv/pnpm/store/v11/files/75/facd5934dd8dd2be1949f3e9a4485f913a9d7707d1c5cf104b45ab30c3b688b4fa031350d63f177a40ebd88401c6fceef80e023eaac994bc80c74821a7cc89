import { type DecodingMode, decodeHTML, decodeXML } from "./decode.js";
import { encodeHTML, encodeNonAsciiHTML } from "./encode.js";
import {
    encodeXML,
    escapeAttribute,
    escapeText,
    escapeUTF8,
} from "./escape.js";

/** The level of entities to support. */
export enum EntityLevel {
    /** Support only XML entities. */
    XML = 0,
    /** Support HTML entities, which are a superset of XML entities. */
    HTML = 1,
}

/**
 * Encoding strategy used by `encode`.
 */
export enum EncodingMode {
    /**
     * The output is UTF-8 encoded. Only characters that need escaping within
     * XML will be escaped.
     */
    UTF8,
    /**
     * The output consists only of ASCII characters. Characters that need
     * escaping within HTML, and characters that aren't ASCII characters will
     * be escaped.
     */
    ASCII,
    /**
     * Encode all characters that have an equivalent entity, as well as all
     * characters that are not ASCII characters.
     */
    Extensive,
    /**
     * Encode all characters that have to be escaped in HTML attributes,
     * following {@link https://html.spec.whatwg.org/multipage/parsing.html#escapingString}.
     */
    Attribute,
    /**
     * Encode all characters that have to be escaped in HTML text,
     * following {@link https://html.spec.whatwg.org/multipage/parsing.html#escapingString}.
     */
    Text,
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
export function decode(
    input: string,
    options: DecodingOptions | EntityLevel = EntityLevel.XML,
): string {
    const level = typeof options === "number" ? options : options.level;

    if (level === EntityLevel.HTML) {
        const mode = typeof options === "object" ? options.mode : undefined;
        return decodeHTML(input, mode);
    }

    return decodeXML(input);
}

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
export function encode(
    input: string,
    options: EncodingOptions | EntityLevel = EntityLevel.XML,
): string {
    const { mode = EncodingMode.Extensive, level = EntityLevel.XML } =
        typeof options === "number" ? { level: options } : options;

    switch (mode) {
        case EncodingMode.UTF8: {
            return escapeUTF8(input);
        }
        case EncodingMode.Attribute: {
            return escapeAttribute(input);
        }
        case EncodingMode.Text: {
            return escapeText(input);
        }
        case EncodingMode.ASCII: {
            return level === EntityLevel.HTML
                ? encodeNonAsciiHTML(input)
                : encodeXML(input);
        }
        // biome-ignore lint/complexity/noUselessSwitchCase: we get an error for the switch not being exhaustive
        case EncodingMode.Extensive:
        default: {
            return level === EntityLevel.HTML
                ? encodeHTML(input)
                : encodeXML(input);
        }
    }
}

export {
    DecodingMode,
    decodeHTML,
    decodeHTMLAttribute,
    decodeHTMLStrict,
    decodeXML,
    decodeXML as decodeXMLStrict,
    EntityDecoder,
} from "./decode.js";

export {
    encodeHTML,
    encodeNonAsciiHTML,
} from "./encode.js";
export {
    encodeXML,
    escape,
    escapeAttribute,
    escapeText,
    escapeUTF8,
} from "./escape.js";
