// Adapted from DeepSeek Harness c291e7961a (MIT).
/** Assistant reasoning disclosure, independent of Tool-call presentation. */
import { useState, type ReactNode } from 'react'
import { Atom as IconThinkOutline14 } from 'lucide-react'
import { DisclosureRow } from './DisclosureRow'
import type { useT } from '../../lib/i18n'
const a11yCss = { visuallyHidden: 'sr-only' }
import css from './ReasoningRow.styles'

function firstLine(text: string): string {
  const newline = text.indexOf('\n')
  return newline === -1 ? text : text.slice(0, newline)
}

function latestLine(text: string): string {
  const visible = text.trimEnd()
  const newline = visible.lastIndexOf('\n')
  return newline === -1 ? visible : visible.slice(newline + 1)
}

/**
 * Render one assistant reasoning block as the Think disclosure row. The
 * collapsed summary omits double-asterisk markers; expanded content preserves
 * the complete text.
 * @param props.text - complete or streaming reasoning text.
 * @param props.running - whether this block is the streaming tail.
 * @param props.t - conversation locale seat for the running status.
 * @returns the reasoning disclosure.
 */
export function ReasoningRow({ text, running, t, children, beforeToggle, duration }: { text: string; running: boolean; t: ReturnType<typeof useT>; children?: ReactNode; beforeToggle?: () => void; duration?: string }) {
  const [expanded, setExpanded] = useState(false)
  const summary = (running ? latestLine(text) : firstLine(text.trimStart())).replaceAll('**', '')

  return (
    <div
      className={css.root}
      data-variant="think"
      data-state={running ? 'running' : 'ok'}
      data-expanded={expanded || undefined}
    >
      {running && <span className={a11yCss.visuallyHidden}>{t('chat.running')}</span>}
      <DisclosureRow
        rowClassName={css.row}
        leadingClassName={css.leading}
        titleClassName={css.title}
        chevronClassName={css.chevron}
        icon={<IconThinkOutline14 size={14} />}
        title={t('chat.reasoning')}
        open={expanded}
        expandable
        expandOnRowClick
        onToggle={() => { beforeToggle?.(); setExpanded(value => !value) }}
        collapsedContent={(
          <>
            <span className={css.separator} aria-hidden />
            <span className={css.summary} data-follow-end={running || undefined}>
              <span className={css.summaryText}>{summary}</span>
            </span>
            {duration && !running && <span className="dsh-row-duration">{duration}</span>}
          </>
        )}
      >
        <div className={css.thinkBody}>{children ?? text}</div>
      </DisclosureRow>
    </div>
  )
}
