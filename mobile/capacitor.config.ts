/**
 * Capacitor shell config for iOS / Android packaging.
 * Native projects are generated with `npx cap add ios|android` in a later
 * milestone once signing materials and mobilecore AAR/XCFramework are ready.
 *
 * Typed as a plain object so `@capacitor/cli` is not required until native
 * scaffolding is added.
 */
const config = {
  appId: "ai.reasonix.mobile",
  appName: "Reasonix",
  webDir: "dist",
  server: {
    androidScheme: "https",
  },
  ios: {
    // Product minimum: iOS 17
    preferredContentMode: "mobile",
  },
  android: {
    // Product minimum: Android 10 / API 29 — enforced in native project.
    allowMixedContent: false,
  },
  plugins: {
    // Secure storage and mobilecore plugins land with native scaffolding.
  },
};

export default config;
