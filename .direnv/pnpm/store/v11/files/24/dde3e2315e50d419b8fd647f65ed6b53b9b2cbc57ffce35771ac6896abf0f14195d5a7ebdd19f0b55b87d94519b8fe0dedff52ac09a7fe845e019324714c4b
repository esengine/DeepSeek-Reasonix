import {
  AbstractMermaidValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  WardleyGrammarGeneratedModule,
  __name as __name2,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject
} from "./chunk-A7G5T7E5.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// ../parser/dist/chunks/mermaid-parser.core/chunk-5FCAYU7R.mjs
var WardleyValueConverter = class extends AbstractMermaidValueConverter {
  static {
    __name(this, "WardleyValueConverter");
  }
  static {
    __name2(this, "WardleyValueConverter");
  }
  runCustomConverter(rule, input, _cstNode) {
    switch (rule.name.toUpperCase()) {
      case "LINK_LABEL":
        return input.substring(1).trim();
      default:
        return void 0;
    }
  }
};
var WardleyModule = {
  parser: {
    ValueConverter: /* @__PURE__ */ __name2(() => new WardleyValueConverter(), "ValueConverter")
  }
};
function createWardleyServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const Wardley = inject(
    createDefaultCoreModule({ shared }),
    WardleyGrammarGeneratedModule,
    WardleyModule
  );
  shared.ServiceRegistry.register(Wardley);
  return { shared, Wardley };
}
__name(createWardleyServices, "createWardleyServices");
__name2(createWardleyServices, "createWardleyServices");

export {
  WardleyModule,
  createWardleyServices
};
