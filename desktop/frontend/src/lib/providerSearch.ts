export function providerSupportsServerWebSearch(kind: string, baseUrl: string): boolean {
  try {
    const endpoint = new URL(baseUrl.trim());
    if (
      endpoint.protocol !== "https:" ||
      endpoint.hostname.toLowerCase() !== "api.deepseek.com" ||
      endpoint.port ||
      endpoint.username ||
      endpoint.password ||
      endpoint.search ||
      endpoint.hash
    ) return false;
    const path = endpoint.pathname.replace(/\/+$/, "");
    switch (kind.trim().toLowerCase()) {
      case "openai":
        return path === "" || path === "/v1";
      case "responses":
        return path === "";
      case "anthropic":
        return path === "/anthropic";
      default:
        return false;
    }
  } catch {
    return false;
  }
}
