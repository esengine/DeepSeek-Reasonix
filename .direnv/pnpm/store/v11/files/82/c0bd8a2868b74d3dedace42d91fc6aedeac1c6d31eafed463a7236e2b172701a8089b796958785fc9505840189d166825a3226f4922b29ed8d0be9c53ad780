import {
  AbstractMermaidTokenBuilder,
  AbstractMermaidValueConverter,
  EmptyFileSystem,
  MermaidGeneratedSharedModule,
  TreeViewGrammarGeneratedModule,
  __name,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject,
  lib_exports
} from "./chunk-36B4POZ4.mjs";

// src/language/treeView/valueConverter.ts
var TreeViewValueConverter = class extends AbstractMermaidValueConverter {
  static {
    __name(this, "TreeViewValueConverter");
  }
  runCustomConverter(rule, input, _cstNode) {
    if (rule.name === "INDENTATION") {
      return input?.length || 0;
    }
    if (rule.name === "QUOTED_NAME") {
      return input.substring(1, input.length - 1);
    }
    if (rule.name === "BARE_NAME") {
      return input.replace(/[\t ]+$/, "");
    }
    if (rule.name === "CLASS_ANNOTATION") {
      const trimmed = input.trim();
      return trimmed.substring(3).trim();
    }
    if (rule.name === "ICON_ANNOTATION") {
      const trimmed = input.trim();
      return trimmed.substring(5, trimmed.length - 1);
    }
    if (rule.name === "DESC_ANNOTATION") {
      const trimmed = input.trim();
      return trimmed.substring(2).trim();
    }
    return void 0;
  }
};

// src/language/treeView/tokenBuilder.ts
var TreeViewTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "TreeViewTokenBuilder");
  }
  constructor() {
    super(["treeView-beta"]);
  }
};

// src/language/treeView/module.ts
var TreeViewModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name(() => new TreeViewTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name(() => new TreeViewValueConverter(), "ValueConverter")
  }
};
function createTreeViewServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const TreeView = inject(
    createDefaultCoreModule({ shared }),
    TreeViewGrammarGeneratedModule,
    TreeViewModule
  );
  shared.ServiceRegistry.register(TreeView);
  return { shared, TreeView };
}
__name(createTreeViewServices, "createTreeViewServices");

export {
  TreeViewModule,
  createTreeViewServices
};
