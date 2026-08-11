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
]);
