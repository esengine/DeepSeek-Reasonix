import { h } from "preact";
import { useState, useCallback, useEffect } from "preact/hooks";
import htm from "htm";
import { api, MODE } from "./api.js";

const html = htm.bind(h);

export interface TreeNode {
  name: string;
  path: string;
  isDir: boolean;
  children?: TreeNode[];
}

export interface OpenFile {
  path: string;
  name: string;
  content: string;
  language: string;
}

const EXT_ICONS: Record<string, string> = {
  ts: "TS", tsx: "TS", js: "JS", jsx: "JS",
  json: "{}", css: "#", scss: "#", html: "<>",
  md: "MD", py: "PY", rs: "RS", go: "GO",
  yaml: "Y", yml: "Y", toml: "T", xml: "<>",
  svg: "<>", png: "[]", jpg: "[]", ico: "[]",
  sh: "$", bash: "$", ps1: "$", bat: "$",
  sql: "DB", graphql: "GQ", proto: "PB",
  dockerfile: "D", makefile: "MK",
};

const EXT_LANG: Record<string, string> = {
  ts: "typescript", tsx: "typescript", js: "javascript", jsx: "javascript",
  json: "json", css: "css", scss: "scss", html: "html",
  md: "markdown", py: "python", rs: "rust", go: "go",
  yaml: "yaml", yml: "yaml", toml: "toml", xml: "xml",
  sh: "bash", bash: "bash", ps1: "powershell", bat: "batch",
  sql: "sql", graphql: "graphql", proto: "protobuf",
  dockerfile: "dockerfile", makefile: "makefile",
};

export function getFileIcon(name: string): { icon: string; cls: string } {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  const icon = EXT_ICONS[ext] ?? "·";
  const cls = ext || "file";
  return { icon, cls };
}

export function getLanguage(name: string): string {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  return EXT_LANG[ext] ?? ext;
}

function isBinaryExt(name: string): boolean {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  const binary = new Set(["png", "jpg", "jpeg", "gif", "ico", "svg", "woff", "woff2", "ttf", "eot", "mp4", "webm", "mp3", "wav", "zip", "tar", "gz", "7z", "pdf"]);
  return binary.has(ext);
}

interface ProjectTreeResult {
  tree: TreeNode[];
}

export function useProjectTree() {
  const [tree, setTree] = useState<TreeNode[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [refreshKey, setRefreshKey] = useState(0);

  const refresh = useCallback(() => {
    setRefreshKey((k) => k + 1);
  }, []);

  useEffect(() => {
    if (MODE === "standalone") {
      setLoading(false);
      setTree(createDemoTree());
      return;
    }
    let cancelled = false;
    setLoading(true);
    api<ProjectTreeResult>("/project-tree")
      .then((r) => {
        if (!cancelled) {
          setTree(r.tree);
          setLoading(false);
          setError(null);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setError((err as Error).message);
          setLoading(false);
          setTree(createDemoTree());
        }
      });
    return () => { cancelled = true; };
  }, [refreshKey]);

  return { tree, loading, error, refresh };
}

interface FileReadResult {
  content: string;
  path: string;
  size: number;
}

interface FileLoadingState {
  [path: string]: boolean;
}

export function useFileTreeState(initialTree: TreeNode[]) {
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [openFiles, setOpenFiles] = useState<OpenFile[]>([]);
  const [activeFilePath, setActiveFilePath] = useState<string | null>(null);
  const [loadingFiles, setLoadingFiles] = useState<FileLoadingState>({});

  const toggleExpand = useCallback((path: string) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  }, []);

  const openFile = useCallback(async (node: TreeNode) => {
    if (node.isDir) {
      toggleExpand(node.path);
      return;
    }
    if (isBinaryExt(node.name)) return;

    const existing = openFiles.find((f) => f.path === node.path);
    if (existing) {
      setActiveFilePath(node.path);
      return;
    }

    if (loadingFiles[node.path]) return;

    setLoadingFiles((prev) => ({ ...prev, [node.path]: true }));

    const lang = getLanguage(node.name);

    if (MODE === "standalone") {
      const mockContent = generateMockContent(node.name, lang);
      setOpenFiles((prev) => [...prev, { path: node.path, name: node.name, content: mockContent, language: lang }]);
      setActiveFilePath(node.path);
      setLoadingFiles((prev) => {
        const next = { ...prev };
        delete next[node.path];
        return next;
      });
      return;
    }

    try {
      const encodedPath = node.path.split("/").map(encodeURIComponent).join("/");
      const data = await api<FileReadResult>(`/file/${encodedPath}`);
      setOpenFiles((prev) => [...prev, { path: node.path, name: node.name, content: data.content, language: lang }]);
      setActiveFilePath(node.path);
    } catch (err) {
      console.error(`[file-tree] failed to load ${node.path}:`, err);
      setOpenFiles((prev) => [...prev, { path: node.path, name: node.name, content: `// Failed to load file: ${(err as Error).message}\n`, language: lang }]);
      setActiveFilePath(node.path);
    } finally {
      setLoadingFiles((prev) => {
        const next = { ...prev };
        delete next[node.path];
        return next;
      });
    }
  }, [openFiles, toggleExpand, loadingFiles]);

  const closeFile = useCallback((path: string) => {
    setOpenFiles((prev) => {
      const next = prev.filter((f) => f.path !== path);
      if (activeFilePath === path) {
        const lastFile = next[next.length - 1];
        setActiveFilePath(lastFile ? lastFile.path : null);
      }
      return next;
    });
  }, [activeFilePath]);

  const activeFile = openFiles.find((f) => f.path === activeFilePath) ?? null;

  return { expanded, openFiles, activeFilePath, activeFile, toggleExpand, openFile, closeFile, setActiveFilePath, loadingFiles };
}

function generateMockContent(name: string, lang: string): string {
  const ext = name.split(".").pop()?.toLowerCase() ?? "";
  if (ext === "json") return JSON.stringify({ name: "example", version: "1.0.0", dependencies: { react: "^18.0.0" } }, null, 2);
  if (ext === "md") return "# Example Document\n\nThis is a sample markdown file.\n\n## Section\n\n- Item 1\n- Item 2\n\n```js\nconsole.log(\"hello\");\n```";
  if (ext === "css") return "/* styles */\n.container {\n  display: flex;\n  padding: 16px;\n  color: var(--fg-1);\n}";
  if (ext === "html") return "<!doctype html>\n<html>\n<head><title>Example</title></head>\n<body>\n  <h1>Hello</h1>\n</body>\n</html>";
  if (ext === "py") return "def hello():\n    print('Hello, World!')\n\nif __name__ == '__main__':\n    hello()";
  if (ext === "yaml" || ext === "yml") return "name: example\nversion: '1.0'\nservices:\n  app:\n    image: node:18\n    ports:\n      - '3000:3000'";
  return `// ${name}\n// Language: ${lang}\n\nexport function example() {\n  return \"Hello from ${name}\";\n}\n`;
}

export function createDemoTree(): TreeNode[] {
  return [
    {
      name: "src", path: "src", isDir: true, children: [
        { name: "index.ts", path: "src/index.ts", isDir: false },
        { name: "app.tsx", path: "src/app.tsx", isDir: false },
        { name: "config.ts", path: "src/config.ts", isDir: false },
        {
          name: "components", path: "src/components", isDir: true, children: [
            { name: "Header.tsx", path: "src/components/Header.tsx", isDir: false },
            { name: "Sidebar.tsx", path: "src/components/Sidebar.tsx", isDir: false },
            { name: "Button.tsx", path: "src/components/Button.tsx", isDir: false },
          ]
        },
        {
          name: "lib", path: "src/lib", isDir: true, children: [
            { name: "api.ts", path: "src/lib/api.ts", isDir: false },
            { name: "format.ts", path: "src/lib/format.ts", isDir: false },
          ]
        },
      ]
    },
    {
      name: "tests", path: "tests", isDir: true, children: [
        { name: "app.test.ts", path: "tests/app.test.ts", isDir: false },
        { name: "utils.test.ts", path: "tests/utils.test.ts", isDir: false },
      ]
    },
    { name: "package.json", path: "package.json", isDir: false },
    { name: "tsconfig.json", path: "tsconfig.json", isDir: false },
    { name: "README.md", path: "README.md", isDir: false },
    { name: "styles.css", path: "styles.css", isDir: false },
    { name: "index.html", path: "index.html", isDir: false },
  ];
}