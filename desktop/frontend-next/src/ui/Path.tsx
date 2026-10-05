import { Fragment } from "react";

/** A path that breaks after its separators.
 *
 *  `word-break: break-all` cuts through the middle of a token, which on a long
 *  workspace path lands inside a number. <wbr> is markup rather than text, so
 *  copying the row still yields the path and nothing else. */
export function Path({ of }: { of: string }) {
  const parts = of.split("/");
  return (
    <>
      {parts.map((seg, i) => (
        <Fragment key={i}>
          {i ? "/" : ""}
          {seg}
          {i < parts.length - 1 && <wbr />}
        </Fragment>
      ))}
    </>
  );
}
