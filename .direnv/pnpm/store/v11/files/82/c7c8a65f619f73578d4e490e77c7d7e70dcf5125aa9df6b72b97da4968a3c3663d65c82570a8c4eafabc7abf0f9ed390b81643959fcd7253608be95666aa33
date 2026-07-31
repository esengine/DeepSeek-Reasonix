"use strict";

const { labelToName, legacyHookDecode } = require("@exodus/bytes/encoding.js");
const { toBase64 } = require("@exodus/bytes/base64.js");
const { MIMEType } = require("whatwg-mimetype");
const DOMException = require("../../../generated/idl/DOMException");
const EventTargetImpl = require("../events/EventTarget-impl").implementation;
const ProgressEvent = require("../../../generated/idl/ProgressEvent");
const { setupForSimpleEventAccessors } = require("../helpers/create-event-accessor");
const { fireAnEvent } = require("../helpers/events");
const { copyToArrayBufferInTargetRealm } = require("../helpers/binary-data");

const READY_STATES = Object.freeze({
  EMPTY: 0,
  LOADING: 1,
  DONE: 2
});

const events = ["loadstart", "progress", "load", "abort", "error", "loadend"];

class FileReaderImpl extends EventTargetImpl {
  constructor(globalObject, args, privateData) {
    super(globalObject, args, privateData);

    this.error = null;
    this.readyState = READY_STATES.EMPTY;
    this.result = null;

    this._globalObject = globalObject;
    this._ownerDocument = globalObject.document;
    this._terminated = false;
  }

  readAsArrayBuffer(file) {
    this._readFile(file, "buffer");
  }
  readAsBinaryString(file) {
    this._readFile(file, "binaryString");
  }
  readAsDataURL(file) {
    this._readFile(file, "dataURL");
  }
  readAsText(file, encodingLabel) {
    this._readFile(file, "text", labelToName(encodingLabel) || "UTF-8");
  }

  abort() {
    if (this.readyState === READY_STATES.EMPTY || this.readyState === READY_STATES.DONE) {
      this.result = null;
      return;
    }

    if (this.readyState === READY_STATES.LOADING) {
      this.readyState = READY_STATES.DONE;
      this.result = null;
    }

    this._terminated = true;
    this._fireProgressEvent("abort");
    this._fireProgressEvent("loadend");
  }

  _fireProgressEvent(name, props) {
    fireAnEvent(name, this, ProgressEvent, props);
  }

  _readFile(file, format, encodingLabel) {
    if (this.readyState === READY_STATES.LOADING) {
      throw DOMException.create(this._globalObject, [
        "The object is in an invalid state.",
        "InvalidStateError"
      ]);
    }

    this.readyState = READY_STATES.LOADING;
    this.result = null;
    this.error = null;

    // Multiple separate tasks are necessary to match the spec's separate "queue a task" invocations.
    setImmediate(() => {
      if (this._terminated) {
        this._terminated = false;
        return;
      }

      this._fireProgressEvent("loadstart");

      setImmediate(() => {
        if (this._terminated) {
          this._terminated = false;
          return;
        }

        if (file._bytes.length > 0) {
          this._fireProgressEvent("progress", {
            lengthComputable: !isNaN(file.size),
            total: file.size,
            loaded: file._bytes.length
          });
        }

        setImmediate(() => {
          if (this._terminated) {
            this._terminated = false;
            return;
          }

          this._setResult(file, format, encodingLabel);
          this.readyState = READY_STATES.DONE;
          this._fireProgressEvent("load");
          this._fireProgressEvent("loadend");
        });
      });
    });
  }

  _setResult(file, format, encodingLabel) {
    switch (format) {
      case "binaryString": {
        // Convert Uint8Array to binary string (each byte as a code point)
        let binaryString = "";
        for (let i = 0; i < file._bytes.length; i++) {
          binaryString += String.fromCharCode(file._bytes[i]);
        }
        this.result = binaryString;
        break;
      }
      case "dataURL": {
        // Spec seems very unclear here; see https://github.com/w3c/FileAPI/issues/104.
        const contentType = MIMEType.parse(file.type) || "application/octet-stream";
        this.result = `data:${contentType};base64,${toBase64(file._bytes)}`;
        break;
      }
      case "text": {
        this.result = legacyHookDecode(file._bytes, encodingLabel);
        break;
      }
      case "buffer":
      default: {
        this.result = copyToArrayBufferInTargetRealm(file._bytes.buffer, this._globalObject);
        break;
      }
    }
  }
}
setupForSimpleEventAccessors(FileReaderImpl.prototype, events);

exports.implementation = FileReaderImpl;
