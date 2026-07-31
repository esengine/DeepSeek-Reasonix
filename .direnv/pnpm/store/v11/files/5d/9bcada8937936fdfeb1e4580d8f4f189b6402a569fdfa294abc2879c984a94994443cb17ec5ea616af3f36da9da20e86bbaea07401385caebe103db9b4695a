export interface IOptions {
  allowIcannDomains: boolean;
  allowPrivateDomains: boolean;
  detectIp: boolean;
  // Detect RFC 6761 / IANA special-use domains and expose the result as
  // `isSpecialUse` on `parse()`. Off by default so the common path stays
  // allocation-free with no extra work; enable it to populate the field.
  detectSpecialUse: boolean;
  extractHostname: boolean;
  mixedInputs: boolean;
  validHosts: string[] | null;
  validateHostname: boolean;
}

function setDefaultsImpl({
  allowIcannDomains = true,
  allowPrivateDomains = false,
  detectIp = true,
  detectSpecialUse = false,
  extractHostname = true,
  mixedInputs = true,
  validHosts = null,
  validateHostname = true,
}: Partial<IOptions>): IOptions {
  return {
    allowIcannDomains,
    allowPrivateDomains,
    detectIp,
    detectSpecialUse,
    extractHostname,
    mixedInputs,
    validHosts,
    validateHostname,
  };
}

const DEFAULT_OPTIONS = /*@__INLINE__*/ setDefaultsImpl({});

export function setDefaults(options?: Partial<IOptions>): IOptions {
  if (options === undefined) {
    return DEFAULT_OPTIONS;
  }

  return /*@__INLINE__*/ setDefaultsImpl(options);
}
