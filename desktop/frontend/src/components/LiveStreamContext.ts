import { createContext } from "react";
import type { LiveStream } from "../lib/useController";

export const LiveStreamContext = createContext<LiveStream | undefined>(undefined);
