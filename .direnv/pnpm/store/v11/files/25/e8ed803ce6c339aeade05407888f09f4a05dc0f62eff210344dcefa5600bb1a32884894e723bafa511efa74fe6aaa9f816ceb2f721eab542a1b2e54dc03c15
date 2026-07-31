import {
  AbstractMermaidTokenBuilder,
  CommonValueConverter,
  CynefinGrammarGeneratedModule,
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

// ../parser/dist/chunks/mermaid-parser.core/chunk-OSBZ3O6U.mjs
var CynefinTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "CynefinTokenBuilder");
  }
  static {
    __name2(this, "CynefinTokenBuilder");
  }
  constructor() {
    super(["cynefin-beta"]);
  }
};
var CynefinModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name2(() => new CynefinTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name2(() => new CommonValueConverter(), "ValueConverter")
  }
};
function createCynefinServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const Cynefin = inject(
    createDefaultCoreModule({ shared }),
    CynefinGrammarGeneratedModule,
    CynefinModule
  );
  shared.ServiceRegistry.register(Cynefin);
  return { shared, Cynefin };
}
__name(createCynefinServices, "createCynefinServices");
__name2(createCynefinServices, "createCynefinServices");

export {
  CynefinModule,
  createCynefinServices
};
