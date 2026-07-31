import remarkGfm from "remark-gfm";
import remarkMath from "remark-math";
import { remarkMathPolicy } from "./remarkMathPolicy";

// One shared parser policy keeps live Markdown and session exports identical.
export const reasonixRemarkPlugins = [remarkGfm, remarkMath, remarkMathPolicy];
export { reasonixRehypePlugins } from "./rehypeReasonixKatex";
