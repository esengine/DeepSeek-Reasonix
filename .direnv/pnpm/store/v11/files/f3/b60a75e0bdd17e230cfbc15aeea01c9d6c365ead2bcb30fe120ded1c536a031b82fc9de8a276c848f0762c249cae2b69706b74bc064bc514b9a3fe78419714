"use strict";

/**
 * Concatenate the given typed arrays into a single `Uint8Array`.
 *
 * @param {Array<TypedArray>} arrays - An array of typed arrays. They can be of any type (but not `DataView` or
 * `ArrayBuffer`).
 * @returns {Uint8Array} - A new `Uint8Array` containing a copy of the concatenated data.
 * @see {@link https://github.com/tc39/proposal-typedarray-concat}
 */
exports.concatTypedArrays = arrays => {
  const totalLength = arrays.reduce((sum, arr) => sum + arr.byteLength, 0);
  const result = new Uint8Array(totalLength);
  let offset = 0;
  for (const arr of arrays) {
    const toSet = arr instanceof Uint8Array ? arr : new Uint8Array(arr.buffer, arr.byteOffset, arr.byteLength);
    result.set(toSet, offset);
    offset += arr.byteLength;
  }
  return result;
};

/**
 * Create a copy of the data in a given `ArrayBuffer`, in a new `ArrayBuffer` created in the target realm.
 *
 * Used when we have some internal data, usually in the Node.js realm, and we need to copy it to the target realm.
 * Compared to {@link copyToArrayBufferInTargetRealmDestructively}, use this when we need to keep the original data
 * around, such as when exposing the data from inside the `Blob` implementation to the user.
 *
 * @param {ArrayBuffer} arrayBuffer - The `ArrayBuffer` to copy the data from.
 * @param {object} newRealm - The target realm's global object, which has `ArrayBuffer` and `Uint8Array` constructor
 * properties.
 * @returns {ArrayBuffer} - A new `ArrayBuffer` containing a copy of the data, using the `ArrayBuffer` constructor from
 * `newRealm`.
 */
exports.copyToArrayBufferInTargetRealm = (arrayBuffer, newRealm) => {
  const newAB = new newRealm.ArrayBuffer(arrayBuffer.byteLength);
  const view = new newRealm.Uint8Array(newAB);
  view.set(new newRealm.Uint8Array(arrayBuffer));
  return newAB;
};

/**
 * Create a copy of the data in a given `ArrayBuffer`, in a new `ArrayBuffer` created in the target realm. The original
 * `ArrayBuffer` is transferred in the process, so its data is no longer usable.
 *
 * Used when we have some internal data, usually in the Node.js realm, and we need to copy it to the target realm.
 * Compared to {@link copyToArrayBufferInTargetRealm}, use this when we don't need to keep the original data around,
 * such as when copying data from the Node.js network or `WebSocket` stack to the jsdom user.
 *
 * @param {ArrayBuffer} arrayBuffer - The `ArrayBuffer` to extract the data from. After calling this function,
 * `arrayBuffer` is no longer usable.
 * @param {object} newRealm - The target realm's global object, which has an `ArrayBuffer` constructor property.
 * @returns {ArrayBuffer} - A new `ArrayBuffer` containing the original data data, using the `ArrayBuffer` constructor
 * from `newRealm`.
 */
exports.copyToArrayBufferInTargetRealmDestructively = (arrayBuffer, newRealm) => {
  if (!newRealm.ArrayBuffer.prototype.transfer) {
    return exports.copyToArrayBufferInTargetRealm(arrayBuffer, newRealm);
  }

  return newRealm.ArrayBuffer.prototype.transfer.call(arrayBuffer);
};
