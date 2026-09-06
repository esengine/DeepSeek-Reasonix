import { AppRuntime } from "./AppRuntime";

/**
 * The application entry is intentionally a composition boundary. Runtime
 * ownership, domain commands and region view models live below this seam;
 * this module must remain free of bridge calls and async coordination.
 */
export default function App() {
  return <AppRuntime />;
}
