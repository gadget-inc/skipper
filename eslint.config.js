import eslint from "@eslint/js";
import prettier from "eslint-config-prettier";
// @ts-expect-error eslint-plugin-only-warn is not typed
import onlyWarn from "eslint-plugin-only-warn";
import tseslint from "typescript-eslint";

export default tseslint.config(
  {
    ignores: [".direnv/**"],
  },
  eslint.configs.recommended,
  tseslint.configs.strictTypeChecked,
  tseslint.configs.stylisticTypeChecked,
  prettier,
  {
    plugins: {
      // eslint-disable-next-line @typescript-eslint/no-unsafe-assignment
      "only-warn": onlyWarn,
    },
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      "@typescript-eslint/no-confusing-void-expression": ["warn", { ignoreVoidReturningFunctions: true }],
      "@typescript-eslint/no-misused-promises": ["warn", { checksVoidReturn: false }],
      "@typescript-eslint/restrict-template-expressions": ["warn", { allowNumber: true }],
    },
  },
);
