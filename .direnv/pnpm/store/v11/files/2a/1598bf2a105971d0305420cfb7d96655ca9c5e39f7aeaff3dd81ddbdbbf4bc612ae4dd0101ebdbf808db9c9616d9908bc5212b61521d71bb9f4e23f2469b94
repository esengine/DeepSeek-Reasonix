import { IconifyJSON } from "@iconify/types";
/**
 * Match character
 */
declare const matchChar: RegExp;
interface IconSetValidationOptions {
  /** Whether validation function will attempt to fix icon set instead of throwing errors. */
  fix?: boolean;
  /** Values for provider and prefix. If missing, validation should add them. */
  prefix?: string;
  provider?: string;
}
/**
 * Validate icon set
 * @returns param obj as IconifyJSON type on success, throw error on failure
 */
declare function validateIconSet(obj: unknown, options?: IconSetValidationOptions): IconifyJSON;
export { IconSetValidationOptions, matchChar, validateIconSet };