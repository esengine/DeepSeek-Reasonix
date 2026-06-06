import { describe, expect, it } from "vitest";
import { toolKindFor } from "../src/tool-kind.js";

describe("toolKindFor", () => {
  it("classifies read tools", () => {
    expect(toolKindFor("read_file")).toBe("read");
    expect(toolKindFor("list_directory")).toBe("read");
    expect(toolKindFor("directory_tree")).toBe("read");
    expect(toolKindFor("get_file_info")).toBe("read");
    expect(toolKindFor("glob")).toBe("read");
  });

  it("classifies edit tools", () => {
    expect(toolKindFor("write_file")).toBe("edit");
    expect(toolKindFor("edit_file")).toBe("edit");
    expect(toolKindFor("delete_file")).toBe("edit");
  });

  it("classifies search tools", () => {
    expect(toolKindFor("search_content")).toBe("search");
    expect(toolKindFor("search_files")).toBe("search");
  });

  it("classifies execute tools", () => {
    expect(toolKindFor("run_command")).toBe("execute");
    expect(toolKindFor("run_background")).toBe("execute");
  });

  it("classifies unknown tools as other", () => {
    expect(toolKindFor("foobar")).toBe("other");
    expect(toolKindFor("")).toBe("other");
  });
});
