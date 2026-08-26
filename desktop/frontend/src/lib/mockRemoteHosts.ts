import type { RemoteHostInput, RemoteHostView } from "./remoteTypes";

export function mockRemoteHostView(id: string, input: RemoteHostInput, previous?: RemoteHostView): RemoteHostView {
  return {
    id,
    label: input.label,
    host: input.host,
    port: input.port,
    user: input.user,
    identityFile: input.identityFile,
    proxyJump: input.proxyJump,
    defaultWorkspace: input.defaultWorkspace,
    serveInstall: input.serveInstall,
    credentialMode: input.credentialMode,
    useSSHConfig: input.useSSHConfig,
    passwordSet: input.password ? true : input.clearPassword ? false : previous?.passwordSet,
    keyPassphraseSet: input.keyPassphrase ? true : input.clearPassphrase ? false : previous?.keyPassphraseSet,
  };
}
