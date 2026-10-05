let open: () => void = () => {};

/** Resolves once the app has something of its own to show. Until then the
 *  boot screen holds, because uncovering an empty shell is not arriving. */
export const settled = new Promise<void>((done) => {
  open = done;
});

export function markSettled() {
  open();
}
