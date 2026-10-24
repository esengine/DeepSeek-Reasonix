import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { Group } from "./Group";
import { SubagentClaims } from "./SubagentClaims";
import { SubagentSteerLimits } from "./SubagentSteerLimits";
import { WriteLease } from "./WriteLease";

export function SessionCoordination({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  return (
    <>
      <Group id="write-lease"
        title={t("多个会话同时写一个项目")}
        hint={t("多个会话在同一个项目里工作时如何互相等待。默认开启乐观模式：没声明写入范围的操作（bash、MCP、没写 write_paths 的子代理）不占住整个工作区，多个会话可以同时工作；关闭后为严格模式，它们会占住整个工作区，其他会话要等它结束。只影响之后新开的会话，已在运行的会话保持它启动时的设置。")}
      >
        <WriteLease port={port} />
      </Group>
      <Group id="subagent-claims"
        title={t("子代理的写入范围")}
        hint={t("后台子代理写文件时如何占用工作区。默认开启乐观模式：没有声明 write_paths 的子代理只占用正在写的路径；关闭后为严格模式，它占住整个工作区，其他写入要等它结束。修改会重建运行时，任务运行期间无法变更。")}
      >
        <SubagentClaims port={port} onChanged={onChanged} />
      </Group>
      <Group id="subagent-steer"
        title={t("给运行中子代理的指示")}
        hint={t("你或模型在子代理还没做完时发给它的补充指示，在它下一次调用工具时读到。这里限制每条的大小和发送的频率，改动立即对正在运行的子代理生效。")}
      >
        <SubagentSteerLimits port={port} />
      </Group>
    </>
  );
}
