import {
  AbstractMermaidTokenBuilder,
  AbstractMermaidValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  RailroadGrammarGeneratedModule,
  __name,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject,
  lib_exports
} from "./chunk-KEIR6QF5.mjs";

// src/language/railroad/tokenBuilder.ts
var RailroadTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "RailroadTokenBuilder");
  }
  constructor() {
    super(["railroad-beta"]);
  }
};

// src/language/railroad/valueConverter.ts
var decodeEscapedString = /* @__PURE__ */ __name((input) => {
  const content = input.slice(1, -1);
  let value = "";
  for (let index = 0; index < content.length; index++) {
    const character = content[index];
    if (character === "\\" && index + 1 < content.length) {
      index++;
      const escaped = content[index];
      switch (escaped) {
        case "n":
          value += "\n";
          break;
        case "r":
          value += "\r";
          break;
        case "t":
          value += "	";
          break;
        default:
          value += escaped;
      }
      continue;
    }
    value += character;
  }
  return value;
}, "decodeEscapedString");
var RailroadValueConverter = class extends AbstractMermaidValueConverter {
  static {
    __name(this, "RailroadValueConverter");
  }
  runConverter(rule, input, cstNode) {
    const value = super.runConverter(rule, input, cstNode);
    if (rule.name === "TITLE" && typeof value === "string") {
      const trimmedValue = value.trim();
      if (trimmedValue.startsWith('"') && trimmedValue.endsWith('"') || trimmedValue.startsWith("'") && trimmedValue.endsWith("'")) {
        return decodeEscapedString(trimmedValue);
      }
    }
    return value;
  }
  runCustomConverter(rule, input, _cstNode) {
    if (rule.name === "RR_STRING") {
      return decodeEscapedString(input);
    }
    return void 0;
  }
};

// src/language/railroad/module.ts
var RailroadModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name(() => new RailroadTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name(() => new RailroadValueConverter(), "ValueConverter")
  }
};
function createRailroadServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const Railroad = inject(
    createDefaultCoreModule({ shared }),
    RailroadGrammarGeneratedModule,
    RailroadModule
  );
  shared.ServiceRegistry.register(Railroad);
  return { shared, Railroad };
}
__name(createRailroadServices, "createRailroadServices");

export {
  RailroadModule,
  createRailroadServices
};
