import {
  AbstractMermaidTokenBuilder,
  CommonValueConverter,
  EmptyFileSystem,
  EventModelingGeneratedModule,
  MermaidGeneratedSharedModule,
  __name,
  createDefaultCoreModule,
  createDefaultSharedCoreModule,
  inject,
  lib_exports
} from "./chunk-36B4POZ4.mjs";

// src/language/eventmodeling/tokenBuilder.ts
var EventModelingTokenBuilder = class extends AbstractMermaidTokenBuilder {
  static {
    __name(this, "EventModelingTokenBuilder");
  }
  constructor() {
    super(["eventmodeling"]);
  }
};

// src/language/eventmodeling/event-modeling-validator.ts
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
var EventModelingValidator = class {
  static {
    __name(this, "EventModelingValidator");
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

// src/language/eventmodeling/module.ts
var EventModelingModule = {
  parser: {
    TokenBuilder: /* @__PURE__ */ __name(() => new EventModelingTokenBuilder(), "TokenBuilder"),
    ValueConverter: /* @__PURE__ */ __name(() => new CommonValueConverter(), "ValueConverter")
  },
  validation: {
    EventModelingValidator: /* @__PURE__ */ __name(() => new EventModelingValidator(), "EventModelingValidator")
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

export {
  EventModelingModule,
  createEventModelingServices
};
