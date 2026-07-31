import { unsafeStringify } from './stringify.js';
import v1 from './v1.js';
import v1ToV6 from './v1ToV6.js';
function v6(options, buf, offset) {
    options ??= {};
    offset ??= 0;
    let bytes = v1({ ...options, _v6: true }, new Uint8Array(16));
    bytes = v1ToV6(bytes);
    if (buf) {
        if (offset < 0 || offset + 16 > buf.length) {
            throw new RangeError(`UUID byte range ${offset}:${offset + 15} is out of buffer bounds`);
        }
        for (let i = 0; i < 16; i++) {
            buf[offset + i] = bytes[i];
        }
        return buf;
    }
    return unsafeStringify(bytes);
}
export default v6;
