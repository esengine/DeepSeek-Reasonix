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
      "Three search modes (picked automatically based on which parameters are set):",
      "1. **Default** (className only): walk project tree for a `.java` file, then fall back to scanning `~/.m2/repository` jars.",
      "2. **With jarKeyword** (className + jarKeyword): same as default, but only scan jars whose path/name contains the keyword — much faster when you know the library.",
      "3. **With jarPath** (className + jarPath): skip both project + .m2 scans, decompile directly from the specified jar file.",
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
        projectRoot: {
          type: "string",
          description:
            "Optional. Override the project root directory for phase-1 file search. Defaults to the current session's workspace root.",
        },
        jarPath: {
          type: "string",
          description:
            'Optional. Exact path to a .jar file. When set, skips project file search and .m2 scan — reads the class directly from this jar and decompiles it. Useful when you know which dependency jar contains the class, e.g. "/home/user/.m2/repository/org/springframework/spring-core/6.1.0/spring-core-6.1.0.jar".',
        },
        jarKeyword: {
          type: "string",
          description:
            'Optional. Only search jars whose filename or path contains this keyword (case-insensitive). Dramatically narrows the scan when you know the library name, e.g. "spring-core", "guava", "mycompany-utils". Ignored when jarPath is also set.',
        },
      },
      required: ["className"],
    },
    parallelSafe: true,
    fn: async (args: {
      className: string;
      projectRoot?: string;
      jarPath?: string;
      jarKeyword?: string;
    }) => {
      const className = (args?.className ?? "").trim();
      if (!className) {
        throw new Error("java_source: `className` is required");
      }

      if (!/^[a-zA-Z_$][\w$]*(\.[a-zA-Z_$][\w$]*)*$/.test(className)) {
        throw new Error(
          `java_source: "${className}" is not a valid fully qualified Java class name. Expected format: \`com.example.MyClass\``,
        );
      }

      const projectRoot = args?.projectRoot?.trim() || opts.projectRoot || process.cwd();
      const finder = new ClassSourceFinder({ projectRoot });

      const jarPath = args?.jarPath?.trim();
      const jarKeyword = args?.jarKeyword?.trim();

      if (jarPath) {
        const result = await finder.findSourceInJar(className, jarPath);
        if (!result.found) {
          return JSON.stringify({
            status: "not-found",
            className,
            message: `Class "${className}" not found in jar:\n  ${jarPath}\n\nMake sure the class name is correct and that the jar contains that entry.`,
          });
        }
        return JSON.stringify({
          status: "found",
          className,
          method: result.method,
          sourcePath: result.sourcePath,
          source: result.source,
        });
      }

      const result = await finder.findSource(className, {
        ...(jarKeyword ? { jarKeyword } : {}),
      });

      if (!result.found) {
        const keywordLine = jarKeyword
          ? `  • Maven .m2 / Gradle cache for jars containing keyword "${jarKeyword}"`
          : "  • Maven .m2 / Gradle cache for all jars";
        const tip = jarKeyword
          ? "Try a different keyword, use `jarPath` with the exact path, or check if the class is in a different library."
          : 'Tip: pass `jarKeyword` (e.g. "spring-core", "guava") to narrow the scan, or `jarPath` with the exact jar path to skip the scan entirely.';
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
