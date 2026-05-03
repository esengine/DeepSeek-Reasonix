// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React, { useState } from "react";
import {
  CharPool,
  type Handle,
  HyperlinkPool,
  StylePool,
  inkCompat,
  mount,
  useKeystroke,
} from "../../renderer/index.js";

interface Item {
  readonly value: string;
  readonly label: string;
  readonly hint?: string;
  readonly disabled?: boolean;
}

const ITEMS: ReadonlyArray<Item> = [
  { value: "deepseek-v3", label: "DeepSeek V3", hint: "default · cheap · fast" },
  { value: "deepseek-r1", label: "DeepSeek R1", hint: "reasoning model" },
  { value: "deepseek-coder", label: "DeepSeek Coder", hint: "code-focused", disabled: true },
  { value: "kimi-k2", label: "Kimi K2", hint: "alternate provider" },
];

function findEnabled(items: ReadonlyArray<Item>, from: number, step: -1 | 1): number {
  if (items.length === 0) return 0;
  let i = from;
  for (let tries = 0; tries < items.length; tries++) {
    i = (i + step + items.length) % items.length;
    if (!items[i]?.disabled) return i;
  }
  return from;
}

function SelectRow({ item, active, marker }: { item: Item; active: boolean; marker: string }) {
  const color = item.disabled ? "gray" : active ? "cyan" : undefined;
  return (
    <inkCompat.Box flexDirection="column">
      <inkCompat.Text color={color} bold={active} dimColor={item.disabled}>
        {`${marker} ${item.label}`}
      </inkCompat.Text>
      {item.hint ? (
        <inkCompat.Box paddingLeft={marker.length + 1}>
          <inkCompat.Text dimColor>{item.hint}</inkCompat.Text>
        </inkCompat.Box>
      ) : null}
    </inkCompat.Box>
  );
}

interface SelectProps {
  onSubmit: (value: string) => void;
  onCancel: () => void;
}

function SingleSelect({ onSubmit, onCancel }: SelectProps): React.ReactElement {
  const [index, setIndex] = useState(() =>
    Math.max(
      0,
      ITEMS.findIndex((i) => !i.disabled),
    ),
  );

  useKeystroke((k) => {
    if (k.upArrow) setIndex((i) => findEnabled(ITEMS, i, -1));
    else if (k.downArrow) setIndex((i) => findEnabled(ITEMS, i, +1));
    else if (k.return) {
      const item = ITEMS[index];
      if (item && !item.disabled) onSubmit(item.value);
    } else if (k.escape) onCancel();
  });

  return (
    <inkCompat.Box flexDirection="column" marginY={1}>
      <inkCompat.Text color="cyan" bold>
        Pick a model
      </inkCompat.Text>
      <inkCompat.Box flexDirection="column" marginTop={1}>
        {ITEMS.map((item, i) => (
          <SelectRow
            key={item.value}
            item={item}
            active={i === index}
            marker={i === index ? "▸" : " "}
          />
        ))}
      </inkCompat.Box>
      <inkCompat.Box marginTop={1}>
        <inkCompat.Text dimColor>↑/↓ navigate · Enter confirm · Esc cancel</inkCompat.Text>
      </inkCompat.Box>
    </inkCompat.Box>
  );
}

export interface SelectDemoOptions {
  readonly stdout?: NodeJS.WriteStream;
  readonly stdin?: NodeJS.ReadStream;
}

export async function runSelectDemo(opts: SelectDemoOptions = {}): Promise<void> {
  const stdout = opts.stdout ?? process.stdout;
  const stdin = opts.stdin ?? process.stdin;

  if (!stdin.isTTY || !stdout.isTTY) {
    console.error("select-demo requires an interactive TTY.");
    process.exit(1);
  }

  const pools = {
    char: new CharPool(),
    style: new StylePool(),
    hyperlink: new HyperlinkPool(),
  };

  let resolveExit: (msg: string) => void = () => {};
  const exited = new Promise<string>((resolve) => {
    resolveExit = resolve;
  });

  const handle: Handle = mount(
    <SingleSelect
      onSubmit={(v) => resolveExit(`picked: ${v}`)}
      onCancel={() => resolveExit("(cancelled)")}
    />,
    {
      viewportWidth: stdout.columns ?? 80,
      viewportHeight: stdout.rows ?? 24,
      pools,
      write: (bytes) => stdout.write(bytes),
      stdin,
    },
  );

  const onResize = () => handle.resize(stdout.columns ?? 80, stdout.rows ?? 24);
  stdout.on("resize", onResize);

  let result = "";
  try {
    result = await exited;
  } finally {
    stdout.off("resize", onResize);
    handle.destroy();
    stdin.pause();
  }
  stdout.write(`${result}\n`);
}
