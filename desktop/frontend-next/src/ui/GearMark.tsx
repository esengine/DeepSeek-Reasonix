// Settings, drawn as a gear. A circle with rays is the sun, which is what the
// light-or-dark control and the appearance page already are.
export const GEAR_PATH = "M6.36 3.28 L6.71 1.53 L9.29 1.53 L9.64 3.28 L10.18 3.50 L11.67 2.51 L13.49 4.33 L12.50 5.82 L12.72 6.36 L14.47 6.71 L14.47 9.29 L12.72 9.64 L12.50 10.18 L13.49 11.67 L11.67 13.49 L10.18 12.50 L9.64 12.72 L9.29 14.47 L6.71 14.47 L6.36 12.72 L5.82 12.50 L4.33 13.49 L2.51 11.67 L3.50 10.18 L3.28 9.64 L1.53 9.29 L1.53 6.71 L3.28 6.36 L3.50 5.82 L2.51 4.33 L4.33 2.51 L5.82 3.50Z";

export const GearMark = (
  <>
    <path d={GEAR_PATH} />
    <circle cx="8" cy="8" r="2" />
  </>
);
