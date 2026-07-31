import {
  AbstractMermaidTokenBuilder,
  AbstractMermaidValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  RailroadPegGrammarGeneratedModule,
  __name,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject,
  lib_exports
} from "./chunk-KEIR6QF5.mjs";

// src/language/railroad-peg/tokenBuilder.ts
var RailroadPegTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "RailroadPegTokenBuilder");
  }
  constructor() {
    super(["railroad-peg-beta"]);
  }
};

// src/language/railroad-peg/valueConverter.ts
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
var RailroadPegValueConverter = class extends AbstractMermaidValueConverter {
  static {
    __name(this, "RailroadPegValueConverter");
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
    if (rule.name === "PEG_STRING") {
      return decodeEscapedString(input);
    }
    return void 0;
  }
};

// src/language/railroad-peg/module.ts
var RailroadPegModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name(() => new RailroadPegTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name(() => new RailroadPegValueConverter(), "ValueConverter")
  }
};
function createRailroadPegServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const RailroadPeg = inject(
    createDefaultCoreModule({ shared }),
    RailroadPegGrammarGeneratedModule,
    RailroadPegModule
  );
  shared.ServiceRegistry.register(RailroadPeg);
  return { shared, RailroadPeg };
}
__name(createRailroadPegServices, "createRailroadPegServices");

export {
  RailroadPegModule,
  createRailroadPegServices
};
