import { Box, Text } from "ink";
import React from "react";
import type { Card } from "../state/cards.js";
import { FG } from "../theme/tokens.js";
import { CtxCard } from "./CtxCard.js";
import { DiffCard } from "./DiffCard.js";
import { DoctorCard } from "./DoctorCard.js";
import { ErrorCard } from "./ErrorCard.js";
import { LiveCard } from "./LiveCard.js";
import { MemoryCard } from "./MemoryCard.js";
import { PlanCard } from "./PlanCard.js";
import { ReasoningCard } from "./ReasoningCard.js";
import { SearchCard } from "./SearchCard.js";
import { StreamingCard } from "./StreamingCard.js";
import { SubAgentCard } from "./SubAgentCard.js";
import { TaskCard } from "./TaskCard.js";
import { ToolCard } from "./ToolCard.js";
import { UsageCard } from "./UsageCard.js";
import { UserCard } from "./UserCard.js";
import { WarnCard } from "./WarnCard.js";

// Memoized so the cards array re-rendering (every store update) only
// reconciles cards whose object identity actually changed — the reducer
// keeps prior cards reference-stable, so unchanged history skips work.
export const CardRenderer = React.memo(function CardRenderer({
  card,
}: { card: Card }): React.ReactElement {
  return <Box flexDirection="column">{renderCard(card)}</Box>;
});

function renderCard(card: Card): React.ReactElement {
  switch (card.kind) {
    case "user":
      return <UserCard card={card} />;
    case "reasoning":
      return <ReasoningCard card={card} expanded={true} />;
    case "streaming":
      return <StreamingCard card={card} />;
    case "tool":
      return <ToolCard card={card} />;
    case "task":
      return <TaskCard card={card} />;
    case "plan":
      return <PlanCard card={card} />;
    case "diff":
      return <DiffCard card={card} />;
    case "error":
      return <ErrorCard card={card} />;
    case "warn":
      return <WarnCard card={card} />;
    case "usage":
      return <UsageCard card={card} />;
    case "memory":
      return <MemoryCard card={card} />;
    case "subagent":
      return <SubAgentCard card={card} />;
    case "search":
      return <SearchCard card={card} />;
    case "live":
      return <LiveCard card={card} />;
    case "ctx":
      return <CtxCard card={card} />;
    case "doctor":
      return <DoctorCard card={card} />;
    default:
      return <FallbackCard card={card} />;
  }
}

function FallbackCard({ card }: { card: Card }): React.ReactElement {
  return (
    <Box flexDirection="row">
      <Text color={FG.faint}>{`  · ${card.kind} card · not yet migrated`}</Text>
    </Box>
  );
}
