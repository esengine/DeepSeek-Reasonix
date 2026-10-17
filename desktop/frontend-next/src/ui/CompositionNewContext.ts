import { createContext } from "react";

export const CompositionNewContext = createContext<((id: string) => void) | null>(null);
