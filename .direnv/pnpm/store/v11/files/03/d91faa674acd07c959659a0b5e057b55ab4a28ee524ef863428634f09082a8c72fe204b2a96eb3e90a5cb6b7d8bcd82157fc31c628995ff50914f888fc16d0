// Adapted from https://github.com/mathiasbynens/he/blob/36afe179392226cf1b6ccdb16ebbb7a5a844d93a/src/he.js#L106-L134

const decodeMap = new Map([
    [0, 65_533],
    // C1 Unicode control character reference replacements
    [128, 8364],
    [130, 8218],
    [131, 402],
    [132, 8222],
    [133, 8230],
    [134, 8224],
    [135, 8225],
    [136, 710],
    [137, 8240],
    [138, 352],
    [139, 8249],
    [140, 338],
    [142, 381],
    [145, 8216],
    [146, 8217],
    [147, 8220],
    [148, 8221],
    [149, 8226],
    [150, 8211],
    [151, 8212],
    [152, 732],
    [153, 8482],
    [154, 353],
    [155, 8250],
    [156, 339],
    [158, 382],
    [159, 376],
]);

/**
 * Replace the given code point with a replacement character if it is a
 * surrogate or is outside the valid range. Otherwise return the code
 * point unchanged.
 * @param codePoint Unicode code point to convert.
 */
export function replaceCodePoint(codePoint: number): number {
    if (
        (codePoint >= 0xd8_00 && codePoint <= 0xdf_ff) ||
        codePoint > 0x10_ff_ff
    ) {
        return 0xff_fd;
    }

    return decodeMap.get(codePoint) ?? codePoint;
}
