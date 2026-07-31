"use strict";

const conversions = require("webidl-conversions");
const utils = require("./utils.js");

const CSSStyleSheetInit = require("./CSSStyleSheetInit.js");
const implSymbol = utils.implSymbol;
const ctorRegistrySymbol = utils.ctorRegistrySymbol;
const StyleSheet = require("./StyleSheet.js");

const interfaceName = "CSSStyleSheet";

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
  throw new globalObject.TypeError(`${context} is not of type 'CSSStyleSheet'.`);
};

function makeWrapper(globalObject, newTarget) {
  let proto;
  if (newTarget !== undefined) {
    proto = newTarget.prototype;
  }

  if (!utils.isObject(proto)) {
    proto = globalObject[ctorRegistrySymbol]["CSSStyleSheet"].prototype;
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
  StyleSheet._internalSetup(wrapper, globalObject);
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
  class CSSStyleSheet extends globalObject.StyleSheet {
    constructor() {
      const args = [];
      {
        let curArg = arguments[0];
        curArg = CSSStyleSheetInit.convert(globalObject, curArg, {
          context: "Failed to construct 'CSSStyleSheet': parameter 1"
        });
        args.push(curArg);
      }
      return exports.setup(Object.create(new.target.prototype), globalObject, args);
    }

    insertRule(rule) {
      const esValue = this !== null && this !== undefined ? this : globalObject;
      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'insertRule' called on an object that is not a valid instance of CSSStyleSheet."
        );
      }

      if (arguments.length < 1) {
        throw new globalObject.TypeError(
          `Failed to execute 'insertRule' on 'CSSStyleSheet': 1 argument required, but only ${arguments.length} present.`
        );
      }
      const args = [];
      {
        let curArg = arguments[0];
        curArg = conversions["DOMString"](curArg, {
          context: "Failed to execute 'insertRule' on 'CSSStyleSheet': parameter 1",
          globals: globalObject
        });
        args.push(curArg);
      }
      {
        let curArg = arguments[1];
        if (curArg !== undefined) {
          curArg = conversions["unsigned long"](curArg, {
            context: "Failed to execute 'insertRule' on 'CSSStyleSheet': parameter 2",
            globals: globalObject
          });
        } else {
          curArg = 0;
        }
        args.push(curArg);
      }
      return esValue[implSymbol].insertRule(...args);
    }

    deleteRule(index) {
      const esValue = this !== null && this !== undefined ? this : globalObject;
      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'deleteRule' called on an object that is not a valid instance of CSSStyleSheet."
        );
      }

      if (arguments.length < 1) {
        throw new globalObject.TypeError(
          `Failed to execute 'deleteRule' on 'CSSStyleSheet': 1 argument required, but only ${arguments.length} present.`
        );
      }
      const args = [];
      {
        let curArg = arguments[0];
        curArg = conversions["unsigned long"](curArg, {
          context: "Failed to execute 'deleteRule' on 'CSSStyleSheet': parameter 1",
          globals: globalObject
        });
        args.push(curArg);
      }
      return esValue[implSymbol].deleteRule(...args);
    }

    replace(text) {
      try {
        const esValue = this !== null && this !== undefined ? this : globalObject;
        if (!exports.is(esValue)) {
          throw new globalObject.TypeError(
            "'replace' called on an object that is not a valid instance of CSSStyleSheet."
          );
        }

        if (arguments.length < 1) {
          throw new globalObject.TypeError(
            `Failed to execute 'replace' on 'CSSStyleSheet': 1 argument required, but only ${arguments.length} present.`
          );
        }
        const args = [];
        {
          let curArg = arguments[0];
          curArg = conversions["USVString"](curArg, {
            context: "Failed to execute 'replace' on 'CSSStyleSheet': parameter 1",
            globals: globalObject
          });
          args.push(curArg);
        }
        return utils.tryWrapperForImpl(esValue[implSymbol].replace(...args));
      } catch (e) {
        return globalObject.Promise.reject(e);
      }
    }

    replaceSync(text) {
      const esValue = this !== null && this !== undefined ? this : globalObject;
      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'replaceSync' called on an object that is not a valid instance of CSSStyleSheet."
        );
      }

      if (arguments.length < 1) {
        throw new globalObject.TypeError(
          `Failed to execute 'replaceSync' on 'CSSStyleSheet': 1 argument required, but only ${arguments.length} present.`
        );
      }
      const args = [];
      {
        let curArg = arguments[0];
        curArg = conversions["USVString"](curArg, {
          context: "Failed to execute 'replaceSync' on 'CSSStyleSheet': parameter 1",
          globals: globalObject
        });
        args.push(curArg);
      }
      return esValue[implSymbol].replaceSync(...args);
    }

    addRule() {
      const esValue = this !== null && this !== undefined ? this : globalObject;
      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'addRule' called on an object that is not a valid instance of CSSStyleSheet."
        );
      }
      const args = [];
      {
        let curArg = arguments[0];
        if (curArg !== undefined) {
          curArg = conversions["DOMString"](curArg, {
            context: "Failed to execute 'addRule' on 'CSSStyleSheet': parameter 1",
            globals: globalObject
          });
        } else {
          curArg = "undefined";
        }
        args.push(curArg);
      }
      {
        let curArg = arguments[1];
        if (curArg !== undefined) {
          curArg = conversions["DOMString"](curArg, {
            context: "Failed to execute 'addRule' on 'CSSStyleSheet': parameter 2",
            globals: globalObject
          });
        } else {
          curArg = "undefined";
        }
        args.push(curArg);
      }
      {
        let curArg = arguments[2];
        if (curArg !== undefined) {
          curArg = conversions["unsigned long"](curArg, {
            context: "Failed to execute 'addRule' on 'CSSStyleSheet': parameter 3",
            globals: globalObject
          });
        }
        args.push(curArg);
      }
      return esValue[implSymbol].addRule(...args);
    }

    removeRule() {
      const esValue = this !== null && this !== undefined ? this : globalObject;
      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'removeRule' called on an object that is not a valid instance of CSSStyleSheet."
        );
      }
      const args = [];
      {
        let curArg = arguments[0];
        if (curArg !== undefined) {
          curArg = conversions["unsigned long"](curArg, {
            context: "Failed to execute 'removeRule' on 'CSSStyleSheet': parameter 1",
            globals: globalObject
          });
        } else {
          curArg = 0;
        }
        args.push(curArg);
      }
      return esValue[implSymbol].removeRule(...args);
    }

    get ownerRule() {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'get ownerRule' called on an object that is not a valid instance of CSSStyleSheet."
        );
      }

      return utils.tryWrapperForImpl(esValue[implSymbol]["ownerRule"]);
    }

    get cssRules() {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'get cssRules' called on an object that is not a valid instance of CSSStyleSheet."
        );
      }

      return utils.getSameObject(this, "cssRules", () => {
        return utils.tryWrapperForImpl(esValue[implSymbol]["cssRules"]);
      });
    }

    get rules() {
      const esValue = this !== null && this !== undefined ? this : globalObject;

      if (!exports.is(esValue)) {
        throw new globalObject.TypeError(
          "'get rules' called on an object that is not a valid instance of CSSStyleSheet."
        );
      }

      return utils.getSameObject(this, "rules", () => {
        return utils.tryWrapperForImpl(esValue[implSymbol]["rules"]);
      });
    }
  }
  Object.defineProperties(CSSStyleSheet.prototype, {
    insertRule: { enumerable: true },
    deleteRule: { enumerable: true },
    replace: { enumerable: true },
    replaceSync: { enumerable: true },
    addRule: { enumerable: true },
    removeRule: { enumerable: true },
    ownerRule: { enumerable: true },
    cssRules: { enumerable: true },
    rules: { enumerable: true },
    [Symbol.toStringTag]: { value: "CSSStyleSheet", configurable: true }
  });
  ctorRegistry[interfaceName] = CSSStyleSheet;

  Object.defineProperty(globalObject, interfaceName, {
    configurable: true,
    writable: true,
    value: CSSStyleSheet
  });
};

const Impl = require("../../jsdom/living/css/CSSStyleSheet-impl.js");
