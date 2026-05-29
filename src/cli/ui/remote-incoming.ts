export type RemoteIncoming = {
  handled: boolean;
  text: string;
  fromQQ?: boolean;
  fromTelegram?: boolean;
  fromWeixin?: boolean;
  fromFeishu?: boolean;
} | null;

function isRemoteOrHandled(incoming: RemoteIncoming): boolean {
  if (!incoming) return false;
  return (
    incoming.handled ||
    incoming.fromQQ === true ||
    incoming.fromTelegram === true ||
    incoming.fromWeixin === true ||
    incoming.fromFeishu === true
  );
}

export function resolveRemoteIncoming(
  qqIncoming: RemoteIncoming,
  telegramIncomingFactory: () => RemoteIncoming,
  weixinIncomingFactory: () => RemoteIncoming,
  feishuIncomingFactory: () => RemoteIncoming,
): RemoteIncoming {
  if (isRemoteOrHandled(qqIncoming)) return qqIncoming;

  const telegramIncoming = telegramIncomingFactory();
  if (isRemoteOrHandled(telegramIncoming)) return telegramIncoming;

  const weixinIncoming = weixinIncomingFactory();
  if (isRemoteOrHandled(weixinIncoming)) return weixinIncoming;

  return feishuIncomingFactory();
}
