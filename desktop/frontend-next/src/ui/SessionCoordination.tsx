import { t } from "../i18n";
import type { AgentPort } from "../port/port";
import { Group } from "./Group";
import { SubagentClaims } from "./SubagentClaims";
import { SubagentSteerLimits } from "./SubagentSteerLimits";
import { WriteLease } from "./WriteLease";
import { IsolationResults } from "./IsolationResults";

export function SessionCoordination({ port, onChanged }: { port: AgentPort; onChanged: () => void }) {
  return (
    <>
      <Group id="write-lease"
        title={t("多个会话同时写一个项目")}
        hint={t("选择新会话的协作策略，并查看当前聊天实际采用的策略。未选择新策略时沿用兼容设置，兼容设置默认开启乐观模式；保存不会切换正在运行的操作。")}
      >
        <WriteLease port={port} />
      </Group>
      <Group id="isolation-results" title={t("保留的隔离成果")} hint={t("查看候选与整合尝试，恢复中断的整合，或明确丢弃不再需要的成果。未整合的修改不会按固定天数删除。") }>
        <IsolationResults port={port} />
      </Group>
      <Group id="subagent-claims"
        title={t("子代理的写入范围")}
        hint={t("此项仅控制兼容模式下的子代理预约，默认开启乐观模式：没有声明 write_paths 的子代理只占用正在写的路径。自动、共享和隔离策略使用统一的变更事务。修改会重建运行时，任务运行期间无法变更。")}
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
