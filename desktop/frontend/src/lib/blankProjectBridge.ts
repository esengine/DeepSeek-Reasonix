export interface BlankProjectBindings {
  PickBlankProjectParent(): Promise<string>;
  CreateBlankProject(parentDir: string, projectName: string): Promise<string>;
}

export function makeMockBlankProjectBindings(): BlankProjectBindings {
  return {
    async PickBlankProjectParent() {
      return "~/projects";
    },
    async CreateBlankProject(parentDir: string, projectName: string) {
      const parent = parentDir.replace(/[\\/]+$/, "");
      const name = projectName.trim();
      if (!parent || !name || name === "." || name === ".." || /[\\/]/.test(name)) {
        throw new Error("project name must be a single folder name");
      }
      return `${parent}/${name}`;
    },
  };
}
