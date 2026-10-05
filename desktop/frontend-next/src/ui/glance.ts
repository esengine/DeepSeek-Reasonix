import { useEffect, useState } from "react";

/** Counts the times the window comes back into view: focused again, or its
 *  page made visible. Disk changes made in another application announce
 *  nothing to this one, and coming back is when somebody can have made one. */
export function useGlance(): number {
  const [n, setN] = useState(0);
  useEffect(() => {
    const back = () => {
      if (document.visibilityState !== "hidden") setN((v) => v + 1);
    };
    addEventListener("focus", back);
    document.addEventListener("visibilitychange", back);
    return () => {
      removeEventListener("focus", back);
      document.removeEventListener("visibilitychange", back);
    };
  }, []);
  return n;
}
