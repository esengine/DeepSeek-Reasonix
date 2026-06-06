import codeExcerpt, { type CodeExcerpt } from 'code-excerpt';
import { readFileSync } from 'fs';
import React from 'react';
import StackUtils from 'stack-utils';
import Box from './Box.js';
import Text from './Text.js';

/* eslint-disable custom-rules/no-process-cwd -- stack trace file:// paths are relative to the real OS cwd, not the virtual cwd */

// V8 reports frames as `file:///abs/path/to/file.js`. Strip the
// `file://<cwd>/` prefix so the rendered path is relative to the
// project root — keeps the overlay readable on deep checkouts.
const stripFilePrefix = (path: string | undefined): string | undefined => {
  return path?.replace(`file://${process.cwd()}/`, '');
};

let stackUtilsInstance: StackUtils | undefined;

// Lazy-initialise StackUtils. Building it eagerly at module load would
// snapshot `process.cwd()` before the host has had a chance to chdir,
// producing wrong relative paths when the renderer is consumed as a
// library.
function getStackUtils(): StackUtils {
  return stackUtilsInstance ??= new StackUtils({
    cwd: process.cwd(),
    internals: StackUtils.nodeInternals(),
  });
}

/* eslint-enable custom-rules/no-process-cwd */

type Props = {
  readonly error: Error;
};

export default function ErrorOverview({
  error,
}: Props) {
  const stack = error.stack ? error.stack.split('\n').slice(1) : undefined;
  const origin = stack ? getStackUtils().parseLine(stack[0]!) : undefined;
  const filePath = stripFilePrefix(origin?.file);
  let excerpt: CodeExcerpt[] | undefined;
  let lineWidth = 0;
  if (filePath && origin?.line) {
    try {
      // eslint-disable-next-line custom-rules/no-sync-fs -- sync render path; error overlay can't go async without suspense restructuring
      const sourceCode = readFileSync(filePath, 'utf8');
      excerpt = codeExcerpt(sourceCode, origin.line);
      if (excerpt) {
        for (const {
          line
        } of excerpt) {
          lineWidth = Math.max(lineWidth, String(line).length);
        }
      }
    } catch {
      // file not readable — skip source context
    }
  }
  return <Box flexDirection="column" padding={1}>
      <Box>
        <Text backgroundColor="ansi:red" color="ansi:white">
          {' '}
          ERROR{' '}
        </Text>

        <Text> {error.message}</Text>
      </Box>

      {origin && filePath && <Box marginTop={1}>
          <Text dim>
            {filePath}:{origin.line}:{origin.column}
          </Text>
        </Box>}

      {origin && excerpt && <Box marginTop={1} flexDirection="column">
          {excerpt.map(({
        line: line_0,
        value
      }) => <Box key={line_0}>
              <Box width={lineWidth + 1}>
                <Text dim={line_0 !== origin.line} backgroundColor={line_0 === origin.line ? 'ansi:red' : undefined} color={line_0 === origin.line ? 'ansi:white' : undefined}>
                  {String(line_0).padStart(lineWidth, ' ')}:
                </Text>
              </Box>

              <Text key={line_0} backgroundColor={line_0 === origin.line ? 'ansi:red' : undefined} color={line_0 === origin.line ? 'ansi:white' : undefined}>
                {' ' + value}
              </Text>
            </Box>)}
        </Box>}

      {error.stack && <Box marginTop={1} flexDirection="column">
          {error.stack.split('\n').slice(1).map(line_1 => {
        const parsedLine = getStackUtils().parseLine(line_1);

        // If the line from the stack cannot be parsed, we print out the unparsed line.
        if (!parsedLine) {
          return <Box key={line_1}>
                    <Text dim>- </Text>
                    <Text bold>{line_1}</Text>
                  </Box>;
        }
        return <Box key={line_1}>
                  <Text dim>- </Text>
                  <Text bold>{parsedLine.function}</Text>
                  <Text dim>
                    {' '}
                    ({stripFilePrefix(parsedLine.file) ?? ''}:{parsedLine.line}:
                    {parsedLine.column})
                  </Text>
                </Box>;
      })}
        </Box>}
    </Box>;
}
