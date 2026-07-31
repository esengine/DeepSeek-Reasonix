/**
 * This file provides support for Unicode range U+1D400 to U+1D7FF,
 * Mathematical Alphanumeric Symbols.
 *
 * Function wideCharacterFont takes a wide character as input and returns
 * the font information necessary to render it properly.
 */

import ParseError from "./ParseError";
import type {FontName} from "./types/fonts";

type WideChar = {
    readonly mathClass: string;
    readonly textClass: string;
    readonly font: Extract<FontName,
        | "Main-Bold"
        | "Math-Italic"
        | "Main-BoldItalic"
        | "Script-Regular"
        | "Fraktur-Regular"
        | "AMS-Regular"
        | "SansSerif-Regular"
        | "SansSerif-Bold"
        | "SansSerif-Italic"
        | "Typewriter-Regular"
    > | "";
};

const boldUpright: WideChar = {
    mathClass: "mathbf",
    textClass: "textbf",
    font: "Main-Bold",
};
const italic: WideChar = {
    mathClass: "mathnormal",
    textClass: "textit",
    font: "Math-Italic",
};
const boldItalic: WideChar = {
    mathClass: "boldsymbol",
    textClass: "boldsymbol",
    font: "Main-BoldItalic",
};
const script: WideChar = {
    mathClass: "mathscr",
    textClass: "textscr",
    font: "Script-Regular",
};
const noFont: WideChar = {mathClass: "", textClass: "", font: ""};
const fraktur: WideChar = {
    mathClass: "mathfrak",
    textClass: "textfrak",
    font: "Fraktur-Regular",
};
const doubleStruck: WideChar = {
    mathClass: "mathbb",
    textClass: "textbb",
    font: "AMS-Regular",
};
const boldFraktur: WideChar = {
    mathClass: "mathboldfrak",
    textClass: "textboldfrak",
    font: "Fraktur-Regular",
};
const sansSerif: WideChar = {
    mathClass: "mathsf",
    textClass: "textsf",
    font: "SansSerif-Regular",
};
const boldSansSerif: WideChar = {
    mathClass: "mathboldsf",
    textClass: "textboldsf",
    font: "SansSerif-Bold",
};
const italicSansSerif: WideChar = {
    mathClass: "mathitsf",
    textClass: "textitsf",
    font: "SansSerif-Italic",
};
const monospace: WideChar = {
    mathClass: "mathtt",
    textClass: "texttt",
    font: "Typewriter-Regular",
};

/**
 * Data below is from https://www.unicode.org/charts/PDF/U1D400.pdf
 * That document sorts characters into groups by font type, say bold or italic.
 *
 * In the arrays below, each object consists of three properties:
 *      * The CSS class of that group when in math mode.
 *      * The CSS class of that group when in text mode.
 *      * The font name, so that KaTeX can get font metrics.
 */

const wideLatinLetterData = [
    boldUpright, boldUpright,  // A-Z, a-z
    italic, italic,            // A-Z, a-z
    boldItalic, boldItalic,   // A-Z, a-z
    // Map fancy A-Z letters to script, not calligraphic.
    // This aligns with unicode-math and math fonts (except Cambria Math).
    script, noFont, // A-Z script, a-z — no font
    noFont, noFont, // A-Z bold script, a-z bold script — no font
    fraktur, fraktur,          // A-Z, a-z
    doubleStruck, doubleStruck, // A-Z double-struck, k double-struck
    // Note that we are using a bold font, but font metrics for regular Fraktur.
    boldFraktur, boldFraktur,  // A-Z, a-z
    sansSerif, sansSerif,      // A-Z, a-z
    boldSansSerif, boldSansSerif, // A-Z, a-z
    italicSansSerif, italicSansSerif, // A-Z, a-z
    noFont, noFont,           // A-Z bold italic sans, a-z bold italic sans - no font
    monospace, monospace,     // A-Z, a-z
] as const;

const wideNumeralData = [
    boldUpright,      // 0-9
    noFont,           // 0-9 double-struck. No KaTeX font.
    sansSerif,        // 0-9
    boldSansSerif,   // 0-9
    monospace,       // 0-9
] as const;

export const wideCharacterFont = (
    wideChar: string,
): WideChar => {

    // IE doesn't support codePointAt(). So work with the surrogate pair.
    const H = wideChar.charCodeAt(0);    // high surrogate
    const L = wideChar.charCodeAt(1);    // low surrogate
    const codePoint = ((H - 0xD800) * 0x400) + (L - 0xDC00) + 0x10000;

    if (0x1D400 <= codePoint && codePoint < 0x1D6A4) {
        // wideLatinLetterData contains exactly 26 chars on each row.
        // So we can calculate the relevant row. No traverse necessary.
        const i = Math.floor((codePoint - 0x1D400) / 26);
        return wideLatinLetterData[i];

    } else if (0x1D7CE <= codePoint && codePoint <= 0x1D7FF) {
        // Numerals, ten per row.
        const i = Math.floor((codePoint - 0x1D7CE) / 10);
        return wideNumeralData[i];

    } else if (codePoint === 0x1D6A5 || codePoint === 0x1D6A6) {
        // dotless i or j
        return wideLatinLetterData[0];

    } else if (0x1D6A6 < codePoint && codePoint < 0x1D7CE) {
        // Greek letters. Not supported, yet.
        return noFont;

    } else {
        // We don't support any wide characters outside 1D400–1D7FF.
        throw new ParseError("Unsupported character: " + wideChar);
    }
};
