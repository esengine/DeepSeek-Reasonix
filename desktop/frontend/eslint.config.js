import tsParser from "@typescript-eslint/parser";
import { defineConfig } from "eslint/config";
import reactHooks from "eslint-plugin-react-hooks";

export default defineConfig([
  {
    files: ["src/**/*.{ts,tsx}"],
    linterOptions: {
      reportUnusedDisableDirectives: "off",
    },
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaFeatures: { jsx: true },
        ecmaVersion: "latest",
        sourceType: "module",
      },
    },
    plugins: {
      "react-hooks": reactHooks,
    },
    rules: {
      "react-hooks/rules-of-hooks": "error",
    },
  },
  {
    files: ["src/app-runtime/**/*.{ts,tsx}", "src/app-shell/**/*.{ts,tsx}",
      "src/app-features/**/*.{ts,tsx}", "src/app-domain/**/*.{ts,tsx}", "src/lib/useCommitted*.ts"],
    rules: { "react-hooks/exhaustive-deps": "error" },
  },
]);
