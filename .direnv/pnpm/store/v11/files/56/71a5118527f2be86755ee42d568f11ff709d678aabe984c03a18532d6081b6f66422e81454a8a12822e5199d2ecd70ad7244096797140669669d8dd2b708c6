"use strict";

const conversions = require("webidl-conversions");
const utils = require("./utils.js");

const implSymbol = utils.implSymbol;
const ctorRegistrySymbol = utils.ctorRegistrySymbol;
const CSSGroupingRule = require("./CSSGroupingRule.js");

const interfaceName = "CSSStyleRule";

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
  throw new globalObject.TypeError(`${context} is not of type 'CSSStyleRule'.`);
};

function makeWrapper(globalObject, newTarget) {
  let proto;
  if (newTarget !== undefined) {
    proto = newTarget.prototype;
  }

  if (!utils.isObject(proto)) {
    proto = globalObject[ctorRegistrySymbol]["CSSStyleRule"].prototype;
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
  CSSGroupingRule._internalSetup(wrapper, globalObject);
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
  class CSSStyleRule extends globalObject.CSSGroupingRule {
    constructor() {
      throw new globalObject.TypeError("Illegal constructor");
    }

    get selectorText() {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'get selectorText' called on an object that is not a valid instance of CSSStyleRule."
        );
      }

      return utils.tryWrapperForImpl(esValue[implSymbol]["selectorText"]);
    }

    set selectorText(V) {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'set selectorText' called on an object that is not a valid instance of CSSStyleRule."
        );
      }

      V = conversions["DOMString"](V, {
        context: "Failed to set the 'selectorText' property on 'CSSStyleRule': The provided value",
        globals: globalObject
      });

      esValue[implSymbol]["selectorText"] = V;
    }

    get style() {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'get style' called on an object that is not a valid instance of CSSStyleRule."
        );
      }

      return utils.getSameObject(this, "style", () => {
        return utils.tryWrapperForImpl(esValue[implSymbol]["style"]);
      });
    }

    set style(V) {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'set style' called on an object that is not a valid instance of CSSStyleRule."
        );
      }

      const Q = esValue["style"];
      if (!utils.isObject(Q)) {
        throw new globalObject.TypeError("Property 'style' is not an object");
      }
      Reflect.set(Q, "cssText", V);
    }
  }
  Object.defineProperties(CSSStyleRule.prototype, {
    selectorText: { enumerable: true },
    style: { enumerable: true },
    [Symbol.toStringTag]: { value: "CSSStyleRule", configurable: true }
  });
  ctorRegistry[interfaceName] = CSSStyleRule;

  Object.defineProperty(globalObject, interfaceName, {
    configurable: true,
    writable: true,
    value: CSSStyleRule
  });
};

const Impl = require("../../jsdom/living/css/CSSStyleRule-impl.js");
