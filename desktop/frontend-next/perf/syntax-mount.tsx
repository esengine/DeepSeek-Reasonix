import { createRoot } from "react-dom/client";
import CodeEditor from "../src/ui/CodeEditor";

// A real editor on a given text, mounted into a given element, for the guard
// that reads what a pack's syntax colours do to it.
export function mount(host: HTMLElement, path: string, value: string, dark: boolean) {
  createRoot(host).render(<CodeEditor path={path} value={value} dark={dark} onChange={() => {}} onSave={() => {}} readOnly />);
}
