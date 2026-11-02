import { createContext, type ReactNode } from "react";

// What the sidebar hands the projects heading to draw: lead above it, in the
// same scroll as the projects so a long team scrolls with them rather than
// pushing them out of reach; filters under it, since the team above is not
// filtered by them.
export const RailSlots = createContext<{ lead?: ReactNode; filters?: ReactNode }>({});
