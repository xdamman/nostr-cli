import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: 'nostr post -f article.md --title "Building on Nostr" --hashtag nostr --hashtag protocol',
    delay: 15,
    typewriter: true,
    charsPerFrame: 0.6,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "", delay: 110 },
  { text: "Publishing article (kind 30023) to 6 relays...", delay: 120, color: COLORS.textDim, indent: 1 },
  { text: "✓ relay.damus.io      112ms", delay: 140, color: COLORS.success, indent: 1 },
  { text: "✓ nos.lol              89ms", delay: 150, color: COLORS.success, indent: 1 },
  { text: "✓ relay.primal.net    156ms", delay: 160, color: COLORS.success, indent: 1 },
  { text: "✓ relay.nostr.band    201ms", delay: 170, color: COLORS.success, indent: 1 },
  { text: "✗ eden.nostr.land    2001ms", delay: 180, color: COLORS.red, indent: 1 },
  { text: "✓ relay.snort.social  178ms", delay: 190, color: COLORS.success, indent: 1 },
  { text: "", delay: 205 },
  { text: "✓ Published to 5/6 relays", delay: 215, color: COLORS.success },
  { text: "Slug: building-on-nostr", delay: 228, color: COLORS.textDim, indent: 1 },
];

export const LongForm: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.longForm.start);
  return <TerminalRenderer lines={rendered} />;
};
