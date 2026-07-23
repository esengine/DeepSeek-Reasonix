type ObjectConstructorWithHasOwn = ObjectConstructor & {
  hasOwn?: (value: object, property: PropertyKey) => boolean;
};

// Safari 15.3 / the matching macOS WebKit used by older Wails shells predates
// Object.hasOwn. react-markdown 10 calls it while validating deprecated props,
// so install the tiny ES2022 primitive before React imports the markdown chunk.
export function installObjectHasOwnPolyfill(target: ObjectConstructorWithHasOwn = Object): void {
  if (typeof target.hasOwn === "function") return;
  Object.defineProperty(target, "hasOwn", {
    configurable: true,
    writable: true,
    value(value: object, property: PropertyKey) {
      return Object.prototype.hasOwnProperty.call(value, property);
    },
  });
}

installObjectHasOwnPolyfill();
