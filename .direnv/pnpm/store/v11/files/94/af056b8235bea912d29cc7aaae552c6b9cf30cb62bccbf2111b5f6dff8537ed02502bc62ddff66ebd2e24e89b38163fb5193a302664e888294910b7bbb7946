import {
  AbstractMermaidTokenBuilder,
  AbstractMermaidValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  RailroadGrammarGeneratedModule,
  __name as __name2,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject
} from "./chunk-A7G5T7E5.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// ../parser/dist/chunks/mermaid-parser.core/chunk-5TONJI2A.mjs
var RailroadTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "RailroadTokenBuilder");
  }
  static {
    __name2(this, "RailroadTokenBuilder");
  }
  constructor() {
    super(["railroad-beta"]);
  }
};
var decodeEscapedString = /* @__PURE__ */ __name2((input) => {
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
  static {
    __name2(this, "RailroadValueConverter");
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
var RailroadModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name2(() => new RailroadTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name2(() => new RailroadValueConverter(), "ValueConverter")
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
__name2(createRailroadServices, "createRailroadServices");

export {
  RailroadModule,
  createRailroadServices
};
