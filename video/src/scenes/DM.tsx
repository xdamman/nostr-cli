import React from "react";
import { COLORS, SCENES } from "../styles";
import { useTerminalLines, type TerminalLine } from "../useTerminalLines";
import { TerminalRenderer } from "../TerminalRenderer";

const lines: TerminalLine[] = [
  {
    text: `nostr dm fiatjaf "Love what you're building!"`,
    delay: 15,
    typewriter: true,
    charsPerFrame: 0.7,
    color: COLORS.command,
    prefix: "prompt",
  },
  { text: "", delay: 85 },
  { text: "Protocol:     NIP-17 (gift wrap)", delay: 95, color: COLORS.label, indent: 1 },
  { text: "Signer:       npub1x7d...k3qf", delay: 108, color: COLORS.textDim, indent: 1 },
  { text: "Recipient:    npub1sg6...e2uf", delay: 118, color: COLORS.textDim, indent: 1 },
  { text: "", delay: 130 },
  { text: "Publishing 2 events (gift wrap + self-copy) to 6 relays...", delay: 140, color: COLORS.textDim, indent: 1 },
  { text: "✓ relay.damus.io      89ms", delay: 155, color: COLORS.success, indent: 1 },
  { text: "✓ nos.lol             102ms", delay: 165, color: COLORS.success, indent: 1 },
  { text: "✓ relay.primal.net    145ms", delay: 175, color: COLORS.success, indent: 1 },
  { text: "✓ Published to 3/3 relays", delay: 190, color: COLORS.success },
];

export const DM: React.FC = () => {
  const rendered = useTerminalLines(lines, SCENES.dm.start);
  return <TerminalRenderer lines={rendered} />;
};
