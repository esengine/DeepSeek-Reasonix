import { type ReactNode, createContext } from "react";

export interface RendererBridge {
  readonly emitStatic: (element: ReactNode) => void;
}

export const RendererBridgeContext = createContext<RendererBridge | null>(null);
