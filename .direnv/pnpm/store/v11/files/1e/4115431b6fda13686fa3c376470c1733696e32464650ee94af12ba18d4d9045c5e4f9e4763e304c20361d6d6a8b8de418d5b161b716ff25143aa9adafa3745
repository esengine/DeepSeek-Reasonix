import {
  AbstractMermaidTokenBuilder,
  AbstractMermaidValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  RailroadAbnfGrammarGeneratedModule,
  __name,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject,
  lib_exports
} from "./chunk-36B4POZ4.mjs";

// src/language/railroad-abnf/tokenBuilder.ts
var RailroadAbnfTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "RailroadAbnfTokenBuilder");
  }
  constructor() {
    super(["railroad-abnf-beta"]);
  }
};

// src/language/railroad-abnf/valueConverter.ts
var RailroadAbnfValueConverter = class extends AbstractMermaidValueConverter {
  static {
    __name(this, "RailroadAbnfValueConverter");
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

// src/language/railroad-abnf/module.ts
var RailroadAbnfModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name(() => new RailroadAbnfTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name(() => new RailroadAbnfValueConverter(), "ValueConverter")
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

export {
  RailroadAbnfModule,
  createRailroadAbnfServices
};
