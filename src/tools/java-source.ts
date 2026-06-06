import { ClassSourceFinder } from "../java/class-source-finder.js";
import type { ToolRegistry } from "../tools.js";

export interface JavaSourceToolOptions {
  projectRoot?: string;
}

export function registerJavaSourceTool(
  registry: ToolRegistry,
  opts: JavaSourceToolOptions = {},
): ToolRegistry {
  registry.register({
    name: "java_source",
    description: [
      "Find and return Java source code by fully-qualified class name.",
      "",
      "Search mode: walk the project tree for a `.java` file, then scan `~/.m2/repository` jars whose filename or path contains `jarKeyword`.",
      "",
      "Returns the source text (or decompiled bytecode) on success, or a clear 'not found' message.",
      "Only call this tool once per class name — it's I/O heavy.",
    ].join("\n"),
    readOnly: true,
    parameters: {
      type: "object",
      properties: {
        className: {
          type: "string",
          description:
            'Fully qualified Java class name, e.g. "com.google.common.collect.Lists" or "org.springframework.web.servlet.DispatcherServlet".',
        },
        jarKeyword: {
          type: "string",
          description:
            'Only search jars whose filename or path contains this keyword (case-insensitive). Keep it short — a narrow substring like "spring-core", "guava", or "mycompany-utils" scans faster and matches more precisely than a long fragment.',
        },
      },
      required: ["className", "jarKeyword"],
    },
    parallelSafe: true,
    fn: async (args: { className: string; jarKeyword: string }) => {
      const className = (args?.className ?? "").trim();
      if (!className) {
        throw new Error("java_source: `className` is required");
      }

      if (!/^[a-zA-Z_$][\w$]*(\.[a-zA-Z_$][\w$]*)*$/.test(className)) {
        throw new Error(
          `java_source: "${className}" is not a valid fully qualified Java class name. Expected format: \`com.example.MyClass\``,
        );
      }

      const jarKeyword = args.jarKeyword.trim();
      if (!jarKeyword) {
        throw new Error("java_source: `jarKeyword` must not be empty");
      }

      const projectRoot = opts.projectRoot || process.cwd();
      const finder = new ClassSourceFinder({ projectRoot });

      const result = await finder.findSource(className, { jarKeyword });

      if (!result.found) {
        const keywordLine = `  • Maven .m2 / Gradle cache for jars containing keyword "${jarKeyword}"`;
        const tip = "Try a different keyword, or check if the class is in a different library.";
        return JSON.stringify({
          status: "not-found",
          className,
          message: `No source found for "${className}". Searched:\n  • ${projectRoot}/ for matching .java files\n  ${keywordLine}\n\n${tip}`,
        });
      }

      return JSON.stringify({
        status: "found",
        className,
        method: result.method,
        sourcePath: result.sourcePath,
        source: result.source,
      });
    },
  });

  return registry;
}
