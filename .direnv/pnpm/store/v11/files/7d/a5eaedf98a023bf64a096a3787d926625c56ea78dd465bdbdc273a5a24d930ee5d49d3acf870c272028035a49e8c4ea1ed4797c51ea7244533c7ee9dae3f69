"use strict";
const conversions = require("webidl-conversions");
const { utf8Encode } = require("../helpers/encoding");

// https://fetch.spec.whatwg.org/#concept-bodyinit-extract (note: always UTF-8)
// https://html.spec.whatwg.org/multipage/form-control-infrastructure.html#multipart%2Fform-data-encoding-algorithm
// https://andreubotella.github.io/multipart-form-data/

const contentDispositionPrefix = utf8Encode(`Content-Disposition: form-data; name="`);
const filenamePrefix = utf8Encode(`; filename="`);
const contentType = utf8Encode(`Content-Type: `);

// https://andreubotella.github.io/multipart-form-data/#multipart-form-data-boundary
function generateBoundary() {
  let boundary = "--------------------------";
  for (let i = 0; i < 24; ++i) {
    boundary += Math.floor(Math.random() * 10).toString(16);
  }
  return utf8Encode(boundary);
}

// https://andreubotella.github.io/multipart-form-data/#escape-a-multipart-form-data-name
function escapeName(name, isFilename = false) {
  if (isFilename) {
    name = conversions.USVString(name);
  } else {
    name = name.replace(/\r(?!\n)|(?<!\r)\n/g, "\r\n");
  }

  const encoded = utf8Encode(name);
  const encodedWithSubs = [];
  for (const originalByte of encoded) {
    if (originalByte === 0x0A) {
      encodedWithSubs.push(37, 48, 65); // `%0A`
    } else if (originalByte === 0x0D) {
      encodedWithSubs.push(37, 48, 68); // `%0D`
    } else if (originalByte === 0x22) {
      encodedWithSubs.push(37, 50, 50); // `%22`
    } else {
      encodedWithSubs.push(originalByte);
    }
  }

  return new Uint8Array(encodedWithSubs);
}

// https://andreubotella.github.io/multipart-form-data/#multipart-form-data-chunk-serializer
exports.serializeEntryList = entries => {
  const boundary = generateBoundary();
  const outputChunks = [];

  for (const entry of entries) {
    const chunkBytes = [
      45, 45, // `--`
      ...boundary,
      0x0D, 0x0A
    ];

    chunkBytes.push(...contentDispositionPrefix, ...escapeName(entry.name), 0x22 /* `"` */);

    let { value } = entry;
    if (typeof value === "string") {
      chunkBytes.push(0x0D, 0x0A, 0x0D, 0x0A);

      value = value.replace(/\r(?!\n)|(?<!\r)\n/g, "\r\n");

      chunkBytes.push(...utf8Encode(value));

      chunkBytes.push(0x0D, 0x0A);

      outputChunks.push(new Uint8Array(chunkBytes));
    } else {
      // value is a FileImpl object

      chunkBytes.push(...filenamePrefix, ...escapeName(value.name, true), 0x22, 0x0D, 0x0A);

      const type = value.type !== "" ? value.type : "application/octet-stream";
      chunkBytes.push(...contentType, ...utf8Encode(type));

      chunkBytes.push(0x0D, 0x0A, 0x0D, 0x0A);

      outputChunks.push(
        new Uint8Array(chunkBytes),
        // The spec returns the File object here but for our purposes the bytes (as a `Uint8Array`) are more convenient.
        value._bytes,
        new Uint8Array([0x0D, 0x0A])
      );
    }
  }

  outputChunks.push(
    new Uint8Array([45, 45]), // `--`
    boundary,
    new Uint8Array([45, 45]), // `--`
    new Uint8Array([0x0D, 0x0A])
  );

  return { boundary, outputChunks };
};
