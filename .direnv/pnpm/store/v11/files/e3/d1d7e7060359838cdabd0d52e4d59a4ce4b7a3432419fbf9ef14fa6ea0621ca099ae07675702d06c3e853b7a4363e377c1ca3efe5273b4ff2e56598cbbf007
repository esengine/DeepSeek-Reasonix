import {
  AbstractMermaidTokenBuilder,
  CommonValueConverter,
  CynefinGrammarGeneratedModule,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  __name,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject,
  lib_exports
} from "./chunk-KEIR6QF5.mjs";

// src/language/cynefin/tokenBuilder.ts
var CynefinTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "CynefinTokenBuilder");
  }
  constructor() {
    super(["cynefin-beta"]);
  }
};

// src/language/cynefin/module.ts
var CynefinModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name(() => new CynefinTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name(() => new CommonValueConverter(), "ValueConverter")
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

export {
  CynefinModule,
  createCynefinServices
};
