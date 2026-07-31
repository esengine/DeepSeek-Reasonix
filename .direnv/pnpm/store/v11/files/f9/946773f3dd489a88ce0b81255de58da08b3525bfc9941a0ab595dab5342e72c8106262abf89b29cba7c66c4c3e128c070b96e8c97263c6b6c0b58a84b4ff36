import {
  AbstractMermaidTokenBuilder,
  AbstractMermaidValueConverter,
  ArchitectureGrammarGeneratedModule,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  __name as __name2,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject
} from "./chunk-A7G5T7E5.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// ../parser/dist/chunks/mermaid-parser.core/chunk-MOZMSUNE.mjs
var ArchitectureTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "ArchitectureTokenBuilder");
  }
  static {
    __name2(this, "ArchitectureTokenBuilder");
  }
  constructor() {
    super(["architecture"]);
  }
};
var ArchitectureValueConverter = class extends AbstractMermaidValueConverter {
  static {
    __name(this, "ArchitectureValueConverter");
  }
  static {
    __name2(this, "ArchitectureValueConverter");
  }
  runCustomConverter(rule, input, _cstNode) {
    if (rule.name === "ARCH_ICON") {
      return input.replace(/[()]/g, "").trim();
    } else if (rule.name === "ARCH_TEXT_ICON") {
      return input.replace(/["()]/g, "");
    } else if (rule.name === "ARCH_TITLE") {
      let result = input.replace(/^\[|]$/g, "").trim();
      if (result.startsWith('"') && result.endsWith('"') || result.startsWith("'") && result.endsWith("'")) {
        result = result.slice(1, -1);
        result = result.replace(/\\"/g, '"').replace(/\\'/g, "'");
      }
      return result.trim();
    }
    return void 0;
  }
};
var ArchitectureModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name2(() => new ArchitectureTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name2(() => new ArchitectureValueConverter(), "ValueConverter")
  }
};
function createArchitectureServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const Architecture = inject(
    createDefaultCoreModule({ shared }),
    ArchitectureGrammarGeneratedModule,
    ArchitectureModule
  );
  shared.ServiceRegistry.register(Architecture);
  return { shared, Architecture };
}
__name(createArchitectureServices, "createArchitectureServices");
__name2(createArchitectureServices, "createArchitectureServices");

export {
  ArchitectureModule,
  createArchitectureServices
};
