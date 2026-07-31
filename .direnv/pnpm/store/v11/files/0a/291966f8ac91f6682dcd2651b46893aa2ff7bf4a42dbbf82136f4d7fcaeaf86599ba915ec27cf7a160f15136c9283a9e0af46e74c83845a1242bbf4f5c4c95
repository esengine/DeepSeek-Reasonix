"use strict";

const conversions = require("webidl-conversions");
const utils = require("./utils.js");

const implSymbol = utils.implSymbol;
const ctorRegistrySymbol = utils.ctorRegistrySymbol;
const CSSConditionRule = require("./CSSConditionRule.js");

const interfaceName = "CSSMediaRule";

exports.is = value => {
  return utils.isObject(value) && Object.hasOwn(value, implSymbol) && value[implSymbol] instanceof Impl.implementation;
};
exports.isImpl = value => {
  return utils.isObject(value) && value instanceof Impl.implementation;
};
exports.convert = (globalObject, value, { context = "The provided value" } = {}) => {
  if (exports.is(value)) {
    return utils.implForWrapper(value);
  }
  throw new globalObject.TypeError(`${context} is not of type 'CSSMediaRule'.`);
};

function makeWrapper(globalObject, newTarget) {
  let proto;
  if (newTarget !== undefined) {
    proto = newTarget.prototype;
  }

  if (!utils.isObject(proto)) {
    proto = globalObject[ctorRegistrySymbol]["CSSMediaRule"].prototype;
  }

  return Object.create(proto);
}

exports.create = (globalObject, constructorArgs, privateData) => {
  const wrapper = makeWrapper(globalObject);
  return exports.setup(wrapper, globalObject, constructorArgs, privateData);
};

exports.createImpl = (globalObject, constructorArgs, privateData) => {
  const wrapper = exports.create(globalObject, constructorArgs, privateData);
  return utils.implForWrapper(wrapper);
};

exports._internalSetup = (wrapper, globalObject) => {
  CSSConditionRule._internalSetup(wrapper, globalObject);
};

exports.setup = (wrapper, globalObject, constructorArgs = [], privateData = {}) => {
  privateData.wrapper = wrapper;

  exports._internalSetup(wrapper, globalObject);
  Object.defineProperty(wrapper, implSymbol, {
    value: new Impl.implementation(globalObject, constructorArgs, privateData),
    configurable: true
  });

  wrapper[implSymbol][utils.wrapperSymbol] = wrapper;
  if (Impl.init) {
    Impl.init(wrapper[implSymbol]);
  }
  return wrapper;
};

exports.new = (globalObject, newTarget) => {
  const wrapper = makeWrapper(globalObject, newTarget);

  exports._internalSetup(wrapper, globalObject);
  Object.defineProperty(wrapper, implSymbol, {
    value: Object.create(Impl.implementation.prototype),
    configurable: true
  });

  wrapper[implSymbol][utils.wrapperSymbol] = wrapper;
  if (Impl.init) {
    Impl.init(wrapper[implSymbol]);
  }
  return wrapper[implSymbol];
};

const exposed = new Set(["Window"]);

exports.install = (globalObject, globalNames) => {
  if (!globalNames.some(globalName => exposed.has(globalName))) {
    return;
  }

  const ctorRegistry = utils.initCtorRegistry(globalObject);
  class CSSMediaRule extends globalObject.CSSConditionRule {
    constructor() {
      throw new globalObject.TypeError("Illegal constructor");
    }

    get media() {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'get media' called on an object that is not a valid instance of CSSMediaRule."
        );
      }

      return utils.getSameObject(this, "media", () => {
        return utils.tryWrapperForImpl(esValue[implSymbol]["media"]);
      });
    }

    set media(V) {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'set media' called on an object that is not a valid instance of CSSMediaRule."
        );
      }

      const Q = esValue["media"];
      if (!utils.isObject(Q)) {
        throw new globalObject.TypeError("Property 'media' is not an object");
      }
      Reflect.set(Q, "mediaText", V);
    }

    get matches() {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'get matches' called on an object that is not a valid instance of CSSMediaRule."
        );
      }

      return esValue[implSymbol]["matches"];
    }
  }
  Object.defineProperties(CSSMediaRule.prototype, {
    media: { enumerable: true },
    matches: { enumerable: true },
    [Symbol.toStringTag]: { value: "CSSMediaRule", configurable: true }
  });
  ctorRegistry[interfaceName] = CSSMediaRule;

  Object.defineProperty(globalObject, interfaceName, {
    configurable: true,
    writable: true,
    value: CSSMediaRule
  });
};

const Impl = require("../../jsdom/living/css/CSSMediaRule-impl.js");
