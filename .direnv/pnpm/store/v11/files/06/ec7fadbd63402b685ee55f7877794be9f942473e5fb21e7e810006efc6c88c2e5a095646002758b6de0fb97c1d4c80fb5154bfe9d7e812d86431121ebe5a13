import {
  AbstractMermaidTokenBuilder,
  AbstractMermaidValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  RailroadAbnfGrammarGeneratedModule,
  __name as __name2,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject
} from "./chunk-A7G5T7E5.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// ../parser/dist/chunks/mermaid-parser.core/chunk-5HE753X5.mjs
var RailroadAbnfTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "RailroadAbnfTokenBuilder");
  }
  static {
    __name2(this, "RailroadAbnfTokenBuilder");
  }
  constructor() {
    super(["railroad-abnf-beta"]);
  }
};
var RailroadAbnfValueConverter = class extends AbstractMermaidValueConverter {
  static {
    __name(this, "RailroadAbnfValueConverter");
  }
  static {
    __name2(this, "RailroadAbnfValueConverter");
  }
  runConverter(rule, input, cstNode) {
    const value = super.runConverter(rule, input, cstNode);
    if (rule.name === "TITLE" && typeof value === "string") {
      const trimmedValue = value.trim();
      if (trimmedValue.startsWith('"') && trimmedValue.endsWith('"') || trimmedValue.startsWith("'") && trimmedValue.endsWith("'")) {
        return trimmedValue.slice(1, -1);
      }
    }
    return value;
  }
  runCustomConverter(rule, input, _cstNode) {
    if (rule.name === "ABNF_STRING") {
      return input.slice(1, -1);
    }
    return void 0;
  }
};
var RailroadAbnfModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name2(() => new RailroadAbnfTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name2(() => new RailroadAbnfValueConverter(), "ValueConverter")
  }
};
function createRailroadAbnfServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const RailroadAbnf = inject(
    createDefaultCoreModule({ shared }),
    RailroadAbnfGrammarGeneratedModule,
    RailroadAbnfModule
  );
  shared.ServiceRegistry.register(RailroadAbnf);
  return { shared, RailroadAbnf };
}
__name(createRailroadAbnfServices, "createRailroadAbnfServices");
__name2(createRailroadAbnfServices, "createRailroadAbnfServices");

export {
  RailroadAbnfModule,
  createRailroadAbnfServices
};
