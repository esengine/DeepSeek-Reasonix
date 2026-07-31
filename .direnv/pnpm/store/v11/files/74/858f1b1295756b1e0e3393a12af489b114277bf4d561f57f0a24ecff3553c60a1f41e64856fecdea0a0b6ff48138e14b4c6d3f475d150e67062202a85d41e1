import {
  AbstractMermaidTokenBuilder,
  CommonValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  PacketGrammarGeneratedModule,
  __name as __name2,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject
} from "./chunk-A7G5T7E5.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// ../parser/dist/chunks/mermaid-parser.core/chunk-EMLP6XTP.mjs
var PacketTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "PacketTokenBuilder");
  }
  static {
    __name2(this, "PacketTokenBuilder");
  }
  constructor() {
    super(["packet"]);
  }
};
var PacketModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name2(() => new PacketTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name2(() => new CommonValueConverter(), "ValueConverter")
  }
};
function createPacketServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const Packet = inject(
    createDefaultCoreModule({ shared }),
    PacketGrammarGeneratedModule,
    PacketModule
  );
  shared.ServiceRegistry.register(Packet);
  return { shared, Packet };
}
__name(createPacketServices, "createPacketServices");
__name2(createPacketServices, "createPacketServices");

export {
  PacketModule,
  createPacketServices
};
