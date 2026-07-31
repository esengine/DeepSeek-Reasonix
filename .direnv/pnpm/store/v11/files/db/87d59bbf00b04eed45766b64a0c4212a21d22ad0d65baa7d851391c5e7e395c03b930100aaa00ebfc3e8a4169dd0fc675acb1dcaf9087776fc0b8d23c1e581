import {
  AbstractMermaidTokenBuilder,
  CommonValueConverter,
  EmptyFileSystem,
  EventModelingGeneratedModule,
  MermaidGeneratedSharedModule,
  __name as __name2,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject
} from "./chunk-A7G5T7E5.mjs";
import {
  __name
} from "./chunk-PTVI3W5X.mjs";

// ../parser/dist/chunks/mermaid-parser.core/chunk-5JV3BV7I.mjs
var EventModelingTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "EventModelingTokenBuilder");
  }
  static {
    __name2(this, "EventModelingTokenBuilder");
  }
  constructor() {
    super(["eventmodeling"]);
  }
};
var COMMAND_TYPES = /* @__PURE__ */ new Set(["cmd", "command"]);
var EVENT_TYPES = /* @__PURE__ */ new Set(["evt", "event"]);
var READMODEL_TYPES = /* @__PURE__ */ new Set(["rmo", "readmodel"]);
var PROCESSOR_TYPES = /* @__PURE__ */ new Set(["pcr", "processor"]);
var UI_TYPES = /* @__PURE__ */ new Set(["ui"]);
function registerValidationChecks(services) {
  const validator = services.validation.EventModelingValidator;
  const registry = services.validation.ValidationRegistry;
  if (registry) {
    const checks = {
      EmTimeFrame: validator.checkSourceFrameTypes.bind(validator),
      EmResetFrame: validator.checkSourceFrameTypes.bind(validator)
    };
    registry.register(checks, validator);
  }
}
__name(registerValidationChecks, "registerValidationChecks");
__name2(registerValidationChecks, "registerValidationChecks");
var EventModelingValidator = class {
  static {
    __name(this, "EventModelingValidator");
  }
  static {
    __name2(this, "EventModelingValidator");
  }
  checkSourceFrameTypes(frame, accept) {
    if (frame.sourceFrames.length === 0) {
      return;
    }
    if (COMMAND_TYPES.has(frame.modelEntityType)) {
      this.validateSources(
        frame,
        /* @__PURE__ */ new Set([...UI_TYPES, ...PROCESSOR_TYPES]),
        "command",
        "ui or processor",
        accept
      );
    } else if (EVENT_TYPES.has(frame.modelEntityType)) {
      this.validateSources(frame, COMMAND_TYPES, "event", "command", accept);
    } else if (READMODEL_TYPES.has(frame.modelEntityType)) {
      this.validateSources(frame, EVENT_TYPES, "read model", "event", accept);
    } else if (PROCESSOR_TYPES.has(frame.modelEntityType)) {
      this.validateSources(frame, READMODEL_TYPES, "processor", "read model", accept);
    } else if (UI_TYPES.has(frame.modelEntityType)) {
      this.validateSources(frame, READMODEL_TYPES, "ui", "read model", accept);
    }
  }
  validateSources(frame, allowedSourceTypes, targetLabel, expectedSourceLabel, accept) {
    for (const sourceRef of frame.sourceFrames) {
      const source = sourceRef.ref;
      if (source !== void 0 && !allowedSourceTypes.has(source.modelEntityType)) {
        accept(
          "error",
          `A ${targetLabel} can only receive input from a ${expectedSourceLabel}, not from '${source.modelEntityType}'.`,
          { node: frame, property: "sourceFrames" }
        );
      }
    }
  }
};
var EventModelingModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name2(() => new EventModelingTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name2(() => new CommonValueConverter(), "ValueConverter")
  },
  validation: {
    EventModelingValidator: /* @__PURE__ */ __name2(() => new EventModelingValidator(), "EventModelingValidator")
  }
};
function createEventModelingServices(context = EmptyFileSystem) {
  const shared = inject(
    createDefaultSharedCoreModule(context),
    MermaidGeneratedSharedModule
  );
  const EventModel = inject(
    createDefaultCoreModule({ shared }),
    EventModelingGeneratedModule,
    EventModelingModule
  );
  shared.ServiceRegistry.register(EventModel);
  registerValidationChecks(EventModel);
  return { shared, EventModel };
}
__name(createEventModelingServices, "createEventModelingServices");
__name2(createEventModelingServices, "createEventModelingServices");

export {
  EventModelingModule,
  createEventModelingServices
};
