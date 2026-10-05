import { useEffect, type Dispatch, type SetStateAction } from "react";
import { folded } from "./viewport";

/** Under the scene fold the rail is a drawer laid over the conversation rather
 *  than a column beside it, so going somewhere from it — another session, the
 *  settings — puts it away. Wider, the rail stays where the reader left it. */
export function useDrawerCloses(setRail: Dispatch<SetStateAction<boolean>>, active: string, settings: unknown) {
  useEffect(() => {
    if (folded("scene")) setRail(false);
  }, [active, settings, setRail]);
}
